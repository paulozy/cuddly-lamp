package coverage

import (
	"strings"
	"testing"
)

func TestParseGoCover_HappyPath(t *testing.T) {
	const input = `mode: set
github.com/x/y/a.go:1.1,2.10 1 1
github.com/x/y/a.go:3.1,4.10 1 0
`
	got, err := ParseGoCover(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseGoCover error: %v", err)
	}
	if got.LinesTotal != 2 || got.LinesCovered != 1 {
		t.Fatalf("unexpected counts: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "github.com/x/y/a.go" {
		t.Fatalf("unexpected files: %+v", got.Files)
	}
	if got.Files[0].LinesCovered != 1 || got.Files[0].LinesTotal != 2 {
		t.Fatalf("file counts wrong: %+v", got.Files[0])
	}
}

func TestParseGoCover_PerFileGranularity(t *testing.T) {
	const input = `mode: set
github.com/x/y/a.go:1.1,2.10 2 1
github.com/x/y/a.go:3.1,4.10 1 1
github.com/x/y/b.go:1.1,2.10 3 0
`
	got, err := ParseGoCover(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseGoCover error: %v", err)
	}
	if len(got.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got.Files))
	}
	byPath := map[string]FileCoverage{}
	for _, f := range got.Files {
		byPath[f.Path] = f
	}
	if a := byPath["github.com/x/y/a.go"]; a.LinesCovered != 3 || a.LinesTotal != 3 {
		t.Fatalf("a.go wrong: %+v", a)
	}
	if b := byPath["github.com/x/y/b.go"]; b.LinesCovered != 0 || b.LinesTotal != 3 {
		t.Fatalf("b.go wrong: %+v", b)
	}
}

func TestParseGoCover_AggregatesMultipleStatements(t *testing.T) {
	const input = `mode: set
github.com/x/y/a.go:1.1,5.10 3 1
github.com/x/y/a.go:6.1,8.10 2 0
github.com/x/y/b.go:1.1,2.10 1 1
`
	got, err := ParseGoCover(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseGoCover error: %v", err)
	}
	// covered: 3 + 1 = 4; total: 3 + 2 + 1 = 6
	if got.LinesCovered != 4 || got.LinesTotal != 6 {
		t.Fatalf("unexpected counts: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
}

func TestParseGoCover_Malformed(t *testing.T) {
	const input = `not a coverage profile
random text
`
	_, err := ParseGoCover(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for malformed go cover input")
	}
}

func TestParseLCOV_HappyPath(t *testing.T) {
	const input = `TN:
SF:a.go
DA:1,1
DA:2,0
end_of_record
`
	got, err := ParseLCOV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseLCOV error: %v", err)
	}
	if got.LinesTotal != 2 || got.LinesCovered != 1 {
		t.Fatalf("unexpected counts: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
}

func TestParseLCOV_MultipleFiles(t *testing.T) {
	const input = `TN:
SF:a.go
DA:1,3
DA:2,0
end_of_record
SF:b.go
DA:10,1
DA:11,1
DA:12,0
end_of_record
`
	got, err := ParseLCOV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseLCOV error: %v", err)
	}
	// covered: 3 of 5
	if got.LinesCovered != 3 || got.LinesTotal != 5 {
		t.Fatalf("unexpected counts: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
	if len(got.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got.Files))
	}
	byPath := map[string]FileCoverage{}
	for _, f := range got.Files {
		byPath[f.Path] = f
	}
	if a := byPath["a.go"]; a.LinesCovered != 1 || a.LinesTotal != 2 {
		t.Fatalf("a.go wrong: %+v", a)
	}
	if b := byPath["b.go"]; b.LinesCovered != 2 || b.LinesTotal != 3 {
		t.Fatalf("b.go wrong: %+v", b)
	}
}

func TestParseLCOV_IgnoresGarbage(t *testing.T) {
	const input = `TN:
SF:a.go
BRDA:1,0,0,1
FN:1,foo
DA:1,1
DA:not-a-line
DA:2,0
end_of_record
`
	got, err := ParseLCOV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseLCOV error: %v", err)
	}
	if got.LinesCovered != 1 || got.LinesTotal != 2 {
		t.Fatalf("unexpected counts: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
}

func TestParseCobertura_RootAggregates(t *testing.T) {
	const input = `<?xml version="1.0"?>
<coverage lines-valid="4" lines-covered="2" line-rate="0.5"></coverage>`
	got, err := ParseCobertura(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCobertura error: %v", err)
	}
	if got.LinesTotal != 4 || got.LinesCovered != 2 {
		t.Fatalf("unexpected counts: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
}

func TestParseCobertura_FallsBackToLines(t *testing.T) {
	const input = `<?xml version="1.0"?>
<coverage>
  <packages>
    <package>
      <classes>
        <class>
          <lines>
            <line number="1" hits="2"/>
            <line number="2" hits="0"/>
          </lines>
        </class>
      </classes>
    </package>
  </packages>
</coverage>`
	got, err := ParseCobertura(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCobertura error: %v", err)
	}
	if got.LinesTotal != 2 || got.LinesCovered != 1 {
		t.Fatalf("unexpected counts: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
}

func TestParseCobertura_PerFileFromClasses(t *testing.T) {
	const input = `<?xml version="1.0"?>
<coverage lines-valid="4" lines-covered="2">
  <packages>
    <package>
      <classes>
        <class filename="src/a.py">
          <lines>
            <line number="1" hits="1"/>
            <line number="2" hits="0"/>
          </lines>
        </class>
        <class filename="src/b.py">
          <lines>
            <line number="1" hits="1"/>
            <line number="2" hits="0"/>
          </lines>
        </class>
      </classes>
    </package>
  </packages>
</coverage>`
	got, err := ParseCobertura(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCobertura error: %v", err)
	}
	if len(got.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got.Files))
	}
	byPath := map[string]FileCoverage{}
	for _, f := range got.Files {
		byPath[f.Path] = f
	}
	if _, ok := byPath["src/a.py"]; !ok {
		t.Fatalf("missing src/a.py: %+v", got.Files)
	}
	if _, ok := byPath["src/b.py"]; !ok {
		t.Fatalf("missing src/b.py: %+v", got.Files)
	}
}

func TestParseCobertura_MergesClassesByFilename(t *testing.T) {
	// inner classes / multiple classes per file in JVM-style outputs
	const input = `<?xml version="1.0"?>
<coverage>
  <packages>
    <package>
      <classes>
        <class filename="X.java">
          <lines>
            <line number="1" hits="1"/>
          </lines>
        </class>
        <class filename="X.java">
          <lines>
            <line number="10" hits="0"/>
          </lines>
        </class>
      </classes>
    </package>
  </packages>
</coverage>`
	got, err := ParseCobertura(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCobertura error: %v", err)
	}
	if len(got.Files) != 1 {
		t.Fatalf("expected 1 merged file, got %d: %+v", len(got.Files), got.Files)
	}
	if got.Files[0].LinesCovered != 1 || got.Files[0].LinesTotal != 2 {
		t.Fatalf("merge wrong: %+v", got.Files[0])
	}
}

func TestParseCobertura_RejectsLineRateOnly(t *testing.T) {
	// line-rate alone cannot be safely aggregated (it would synthesize a fake
	// denominator). Parser must reject so the extractor warns the user.
	const input = `<?xml version="1.0"?>
<coverage line-rate="0.42"></coverage>`
	_, err := ParseCobertura(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error when only line-rate is present")
	}
}

func TestParseCobertura_Malformed(t *testing.T) {
	const input = `not xml at all`
	_, err := ParseCobertura(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for malformed cobertura xml")
	}
}

func TestParseJaCoCo_HappyPath(t *testing.T) {
	const input = `<?xml version="1.0"?>
<report name="t">
  <counter type="INSTRUCTION" missed="10" covered="20"/>
  <counter type="LINE" missed="1" covered="1"/>
  <counter type="BRANCH" missed="3" covered="3"/>
</report>`
	got, err := ParseJaCoCo(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseJaCoCo error: %v", err)
	}
	if got.LinesTotal != 2 || got.LinesCovered != 1 {
		t.Fatalf("unexpected counts: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
}

func TestParseJaCoCo_PerSourcefileGranularity(t *testing.T) {
	const input = `<?xml version="1.0"?>
<report name="t">
  <package name="com/foo">
    <sourcefile name="A.java">
      <counter type="INSTRUCTION" missed="2" covered="3"/>
      <counter type="LINE" missed="1" covered="2"/>
    </sourcefile>
    <sourcefile name="B.java">
      <counter type="LINE" missed="3" covered="0"/>
    </sourcefile>
  </package>
  <counter type="LINE" missed="4" covered="2"/>
</report>`
	got, err := ParseJaCoCo(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseJaCoCo error: %v", err)
	}
	if got.LinesCovered != 2 || got.LinesTotal != 6 {
		t.Fatalf("unexpected aggregate: covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
	if len(got.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got.Files))
	}
	byPath := map[string]FileCoverage{}
	for _, f := range got.Files {
		byPath[f.Path] = f
	}
	if a := byPath["com/foo/A.java"]; a.LinesCovered != 2 || a.LinesTotal != 3 {
		t.Fatalf("A.java wrong: %+v", a)
	}
	if b := byPath["com/foo/B.java"]; b.LinesCovered != 0 || b.LinesTotal != 3 {
		t.Fatalf("B.java wrong: %+v", b)
	}
}

func TestParseJaCoCo_NoLineCounter(t *testing.T) {
	const input = `<?xml version="1.0"?>
<report name="t">
  <counter type="INSTRUCTION" missed="10" covered="20"/>
</report>`
	got, err := ParseJaCoCo(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseJaCoCo error: %v", err)
	}
	if got.LinesTotal != 0 || got.LinesCovered != 0 {
		t.Fatalf("expected zero counts when no LINE counter, got covered=%d total=%d", got.LinesCovered, got.LinesTotal)
	}
}

func TestParseJaCoCo_Malformed(t *testing.T) {
	const input = `<not><a><report></not>`
	_, err := ParseJaCoCo(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for malformed jacoco xml")
	}
}
