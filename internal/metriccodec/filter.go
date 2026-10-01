package metriccodec

import "regexp"

// FilterByJob keeps only series whose job label matches re.
// Definitions and samples remain aligned by Series ID.
func FilterByJob(defs []Definition, samples []Sample, re *regexp.Regexp) ([]Definition, []Sample) {
	if re == nil {
		return defs, samples
	}
	allowed := make(map[[16]byte]struct{}, len(defs))
	outDefs := make([]Definition, 0, len(defs))
	for _, d := range defs {
		job := ""
		for _, l := range d.Labels {
			if l.Key == "job" {
				job = l.Value
				break
			}
		}
		if job != "" && re.MatchString(job) {
			allowed[d.ID] = struct{}{}
			outDefs = append(outDefs, d)
		}
	}
	return filterSamples(outDefs, samples, allowed)
}

// FilterByMetricName keeps only metric families whose names match re.
func FilterByMetricName(defs []Definition, samples []Sample, re *regexp.Regexp) ([]Definition, []Sample) {
	if re == nil {
		return defs, samples
	}
	allowed := make(map[[16]byte]struct{}, len(defs))
	outDefs := make([]Definition, 0, len(defs))
	for _, d := range defs {
		if re.MatchString(d.Name) {
			allowed[d.ID] = struct{}{}
			outDefs = append(outDefs, d)
		}
	}
	return filterSamples(outDefs, samples, allowed)
}

func filterSamples(defs []Definition, samples []Sample, allowed map[[16]byte]struct{}) ([]Definition, []Sample) {
	outSamples := make([]Sample, 0, len(samples))
	for _, s := range samples {
		if _, ok := allowed[s.ID]; ok {
			outSamples = append(outSamples, s)
		}
	}
	return defs, outSamples
}
