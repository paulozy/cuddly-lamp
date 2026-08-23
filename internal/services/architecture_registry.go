package services

// The two registries architecture derivation is assembled from.
//
// They live in their own file so adding an ecosystem, a sniff or an edge family
// is one line in one place, and so a test can build an ArchitectureService with
// exactly the extractor it is exercising and nothing else.

func defaultExtractors() []Extractor {
	return []Extractor{
		packagesExtractor{},
		apisExtractor{},
		resourcesExtractor{},
		hostsExtractor{},
	}
}

func defaultEdgeDerivers() []EdgeDeriver {
	return []EdgeDeriver{
		libDepDeriver{},
		consumeDeriver{},
	}
}
