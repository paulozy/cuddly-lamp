package coverage

import "sort"

// Suggestion is a starting point for a repository's CI coverage step: which
// report format its stack usually produces, where that report usually lands, and
// the command that usually produces it.
//
// Every field is a *suggestion*, and the UI keeps all of them editable. That is
// not hedging — it is the only honest shape for this. The platform is
// multi-tenant and monorepos are a first-class case, so "the dominant language is
// Go, therefore `go test ./... -coverprofile=coverage.out`" is right often enough
// to save typing and wrong often enough that presenting it as fact would send
// people to debug a command we invented. Offering a default and letting it be
// corrected costs nothing; asserting it costs trust.
type Suggestion struct {
	// Language is the language the suggestion was derived from, so the UI can
	// say *why* it picked this ("Detectado: Go 98%") instead of appearing to
	// know something it does not.
	Language string `json:"language,omitempty"`
	Format   Format `json:"format"`
	// ReportPath is where the tool conventionally writes the report.
	ReportPath string `json:"report_path"`
	// TestCommand is empty when the ecosystem has no single conventional command
	// worth guessing at. Empty is better than a command that does not exist.
	TestCommand string `json:"test_command,omitempty"`
}

// languageSuggestions maps a lowercased language name to its conventional
// coverage setup.
//
// The keys are lowercased because provider language breakdowns are not
// consistent about case, and `detect.DetectTests` already lowercases for the same
// reason. Only ecosystems with a genuinely conventional answer are listed; a
// language absent from here falls through to the LCOV default rather than getting
// a guessed command.
var languageSuggestions = map[string]Suggestion{
	"go": {
		Format:      FormatGo,
		ReportPath:  "coverage.out",
		TestCommand: "go test ./... -coverprofile=coverage.out -covermode=atomic",
	},
	"javascript": {
		Format:      FormatLCOV,
		ReportPath:  "coverage/lcov.info",
		TestCommand: "npm test -- --coverage",
	},
	"typescript": {
		Format:      FormatLCOV,
		ReportPath:  "coverage/lcov.info",
		TestCommand: "npm test -- --coverage",
	},
	"python": {
		Format:      FormatCobertura,
		ReportPath:  "coverage.xml",
		TestCommand: "pytest --cov --cov-report=xml",
	},
	"java": {
		Format:      FormatJaCoCo,
		ReportPath:  "target/site/jacoco/jacoco.xml",
		TestCommand: "mvn test",
	},
	"kotlin": {
		Format:      FormatJaCoCo,
		ReportPath:  "build/reports/jacoco/test/jacocoTestReport.xml",
		TestCommand: "./gradlew test jacocoTestReport",
	},
	"scala": {
		Format:     FormatCobertura,
		ReportPath: "target/scala-2.13/coverage-report/cobertura.xml",
	},
	"ruby": {
		Format:      FormatLCOV,
		ReportPath:  "coverage/lcov.info",
		TestCommand: "bundle exec rspec",
	},
	"php": {
		Format:      FormatCobertura,
		ReportPath:  "coverage.xml",
		TestCommand: "vendor/bin/phpunit --coverage-cobertura=coverage.xml",
	},
	"c#": {
		Format:      FormatCobertura,
		ReportPath:  "coverage.cobertura.xml",
		TestCommand: "dotnet test --collect:\"XPlat Code Coverage\"",
	},
	"rust": {
		Format:     FormatLCOV,
		ReportPath: "lcov.info",
	},
	"elixir": {
		Format:     FormatLCOV,
		ReportPath: "cover/lcov.info",
	},
	"swift": {
		Format:     FormatLCOV,
		ReportPath: "coverage/lcov.info",
	},
	"dart": {
		Format:      FormatLCOV,
		ReportPath:  "coverage/lcov.info",
		TestCommand: "flutter test --coverage",
	},
}

// defaultSuggestion is what an unrecognized or unknown stack gets.
//
// LCOV rather than "nothing": it is the most widely produced of the four formats
// this platform parses, so it is the best single guess. The command is left empty
// on purpose — there is no command worth guessing for a stack we did not
// recognize, and an invented one is worse than a blank the person fills in.
var defaultSuggestion = Suggestion{Format: FormatLCOV, ReportPath: "coverage/lcov.info"}

// SuggestFromLanguages picks a starting point from a repository's language
// breakdown.
//
// A pure function over the map the sync already stores
// (`RepositoryMetadata.Languages`): no I/O, no context, so every rule is testable
// against a literal map. The dominant language wins.
func SuggestFromLanguages(languages map[string]int) Suggestion {
	for _, name := range dominantLanguages(languages) {
		if suggestion, ok := languageSuggestions[name]; ok {
			suggestion.Language = name
			return suggestion
		}
	}
	return defaultSuggestion
}

// dominantLanguages returns the language names, lowercased, ordered by weight,
// heaviest first.
//
// It returns the whole ordering rather than just the top entry so a repository
// whose heaviest language we have no answer for still gets a suggestion from the
// next one down — a repo that is 60% HTML and 40% Go should suggest Go, not the
// default.
func dominantLanguages(languages map[string]int) []string {
	type entry struct {
		name   string
		weight int
	}
	entries := make([]entry, 0, len(languages))
	for name, weight := range languages {
		entries = append(entries, entry{name: lowerASCII(name), weight: weight})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].weight != entries[j].weight {
			return entries[i].weight > entries[j].weight
		}
		// Ties break alphabetically so the same repository always gets the same
		// answer. A suggestion that flickered between two equally weighted
		// languages across page loads would read as a bug.
		return entries[i].name < entries[j].name
	})

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.name)
	}
	return names
}

// lowerASCII lowercases without pulling in the Unicode tables; language names
// from both providers are ASCII.
func lowerASCII(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

// Formats lists every format the ingest endpoint accepts, for a UI that offers a
// choice. Ordered so the list is stable.
func Formats() []Format {
	return []Format{FormatGo, FormatLCOV, FormatCobertura, FormatJaCoCo}
}
