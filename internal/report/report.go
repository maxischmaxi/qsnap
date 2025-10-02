package report

import (
	"encoding/json"
	"os"
)

type CaseResult struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Status string `json:"status"` // pass | fail | no-baseline | error
	Error  string `json:"error,omitempty"`

	Baseline string `json:"baseline"`
	OutPath  string `json:"outPath"`

	PixelDiff  any `json:"pixelDiff,omitempty"`
	PercepDiff any `json:"percepDiff,omitempty"`
}

type Report struct {
	GeneratedAt string       `json:"generatedAt"`
	Total       int          `json:"total"`
	Passed      int          `json:"passed"`
	Failed      int          `json:"failed"`
	NoBaseline  int          `json:"noBaseline"`
	Errored     int          `json:"errored"`
	Cases       []CaseResult `json:"cases"`
}

func CountStatus(cases []CaseResult, status string) int {
	n := 0
	for _, c := range cases {
		if c.Status == status {
			n++
		}
	}
	return n
}

func Write(path string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
