package coverage

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/tools/cover"
)

// ParseGoCover reads the output of `go test -coverprofile=...`. It uses the
// official Go tools parser, which handles the `set`, `count` and `atomic`
// modes plus overlapping blocks correctly. Each `Profile` corresponds to a
// single source file, so per-file granularity is direct.
func ParseGoCover(r io.Reader) (Report, error) {
	profiles, err := cover.ParseProfilesFromReader(r)
	if err != nil {
		return Report{}, fmt.Errorf("parse go cover profile: %w", err)
	}

	var covered, total int
	files := make([]FileCoverage, 0, len(profiles))
	for _, p := range profiles {
		var fileCovered, fileTotal int
		for _, block := range p.Blocks {
			fileTotal += block.NumStmt
			if block.Count > 0 {
				fileCovered += block.NumStmt
			}
		}
		covered += fileCovered
		total += fileTotal
		files = append(files, FileCoverage{
			Path:         p.FileName,
			LinesCovered: fileCovered,
			LinesTotal:   fileTotal,
		})
	}
	return Report{LinesCovered: covered, LinesTotal: total, Files: files}, nil
}

// ParseLCOV consumes a .lcov / .info tracefile. We rely on per-line `DA:line,hits`
// records — `LF:`/`LH:` aggregates are optional and known to drift in practice.
// `SF:` opens a file block; `end_of_record` closes it.
func ParseLCOV(r io.Reader) (Report, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var covered, total int
	var files []FileCoverage
	var currentFile string
	var fileCovered, fileTotal int

	flushFile := func() {
		if currentFile == "" && fileTotal == 0 {
			return
		}
		files = append(files, FileCoverage{
			Path:         currentFile,
			LinesCovered: fileCovered,
			LinesTotal:   fileTotal,
		})
		currentFile = ""
		fileCovered = 0
		fileTotal = 0
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "SF:"):
			currentFile = strings.TrimPrefix(line, "SF:")
		case line == "end_of_record":
			flushFile()
		case strings.HasPrefix(line, "DA:"):
			payload := strings.TrimPrefix(line, "DA:")
			parts := strings.SplitN(payload, ",", 3)
			if len(parts) < 2 {
				continue
			}
			hits, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				continue
			}
			fileTotal++
			total++
			if hits > 0 {
				fileCovered++
				covered++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Report{}, fmt.Errorf("read lcov: %w", err)
	}
	// Flush trailing file when last `end_of_record` is missing.
	if currentFile != "" || fileTotal > 0 {
		flushFile()
	}
	return Report{LinesCovered: covered, LinesTotal: total, Files: files}, nil
}

type coberturaRoot struct {
	XMLName      xml.Name `xml:"coverage"`
	LinesValid   *int64   `xml:"lines-valid,attr"`
	LinesCovered *int64   `xml:"lines-covered,attr"`
	LineRate     *float64 `xml:"line-rate,attr"`
	Packages     struct {
		Packages []coberturaPackage `xml:"package"`
	} `xml:"packages"`
}

type coberturaPackage struct {
	Classes struct {
		Classes []coberturaClass `xml:"class"`
	} `xml:"classes"`
}

type coberturaClass struct {
	Filename string `xml:"filename,attr"`
	Lines    struct {
		Lines []struct {
			Hits int `xml:"hits,attr"`
		} `xml:"line"`
	} `xml:"lines"`
}

// ParseCobertura reads the root `<coverage>` element. It prefers the
// `lines-valid`/`lines-covered` aggregates; if absent or zero, it falls back
// to walking `<line hits=".."/>` entries. Per-file granularity always uses the
// `<class filename="...">` walk so the PR rule has data to flag missing files.
// The `line-rate` attribute is intentionally NOT used as a fallback —
// synthesizing absolute counts from a fraction would pollute multi-report
// aggregation with fake denominators.
func ParseCobertura(r io.Reader) (Report, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false

	var root coberturaRoot
	if err := dec.Decode(&root); err != nil {
		return Report{}, fmt.Errorf("decode cobertura xml: %w", err)
	}

	// Walk classes for per-file coverage. Multiple classes may share a filename
	// (e.g. inner classes in JVM-style outputs); merge into a single FileCoverage.
	fileMap := make(map[string]*FileCoverage)
	var walkTotal, walkCovered int
	for _, pkg := range root.Packages.Packages {
		for _, cls := range pkg.Classes.Classes {
			fc := fileMap[cls.Filename]
			if fc == nil {
				fc = &FileCoverage{Path: cls.Filename}
				fileMap[cls.Filename] = fc
			}
			for _, ln := range cls.Lines.Lines {
				fc.LinesTotal++
				walkTotal++
				if ln.Hits > 0 {
					fc.LinesCovered++
					walkCovered++
				}
			}
		}
	}
	files := make([]FileCoverage, 0, len(fileMap))
	for _, fc := range fileMap {
		files = append(files, *fc)
	}

	if root.LinesValid != nil && *root.LinesValid > 0 {
		covered := int64(0)
		if root.LinesCovered != nil {
			covered = *root.LinesCovered
		}
		return Report{
			LinesCovered: int(covered),
			LinesTotal:   int(*root.LinesValid),
			Files:        files,
		}, nil
	}

	if walkTotal > 0 {
		return Report{
			LinesCovered: walkCovered,
			LinesTotal:   walkTotal,
			Files:        files,
		}, nil
	}

	return Report{}, fmt.Errorf("cobertura xml has no usable line counts (only line-rate is not enough)")
}

type jacocoReport struct {
	XMLName  xml.Name        `xml:"report"`
	Counters []jacocoCounter `xml:"counter"`
	Packages []jacocoPackage `xml:"package"`
}

type jacocoCounter struct {
	Type    string `xml:"type,attr"`
	Missed  int    `xml:"missed,attr"`
	Covered int    `xml:"covered,attr"`
}

type jacocoPackage struct {
	Name        string                       `xml:"name,attr"`
	Sourcefiles []jacocoSourcefile           `xml:"sourcefile"`
}

type jacocoSourcefile struct {
	Name     string          `xml:"name,attr"`
	Counters []jacocoCounter `xml:"counter"`
}

// ParseJaCoCo reads the `<counter type="LINE" missed=".." covered=".."/>`
// child of `<report>` for the aggregate, and walks `<package>/<sourcefile>`
// entries for per-file granularity. The package `name` attribute is the
// JVM-style path (e.g. `com/foo/bar`); we join it with the sourcefile name
// to produce a path comparable to PR file paths (e.g. `com/foo/bar/X.java`).
func ParseJaCoCo(r io.Reader) (Report, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false

	var root jacocoReport
	if err := dec.Decode(&root); err != nil {
		return Report{}, fmt.Errorf("decode jacoco xml: %w", err)
	}

	var aggCovered, aggTotal int
	for _, c := range root.Counters {
		if strings.EqualFold(c.Type, "LINE") {
			aggCovered = c.Covered
			aggTotal = c.Missed + c.Covered
			break
		}
	}

	var files []FileCoverage
	for _, pkg := range root.Packages {
		for _, sf := range pkg.Sourcefiles {
			path := sf.Name
			if pkg.Name != "" {
				path = pkg.Name + "/" + sf.Name
			}
			fc := FileCoverage{Path: path}
			for _, c := range sf.Counters {
				if strings.EqualFold(c.Type, "LINE") {
					fc.LinesCovered = c.Covered
					fc.LinesTotal = c.Missed + c.Covered
					break
				}
			}
			files = append(files, fc)
		}
	}

	return Report{
		LinesCovered: aggCovered,
		LinesTotal:   aggTotal,
		Files:        files,
	}, nil
}
