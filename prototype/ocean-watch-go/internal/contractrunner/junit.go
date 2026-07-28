package contractrunner

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func writeJUnit(path string, report ComparisonReport) error {
	suite := junitSuite{Name: "ocean-watch-contracts", Tests: report.Total, Failures: report.Failed}
	for _, compared := range report.Cases {
		item := junitCase{Name: compared.ID, Classname: compared.Category}
		if !compared.Passed {
			lines := make([]string, 0, len(compared.Differences))
			for _, difference := range compared.Differences {
				lines = append(lines, difference.Field+": expected "+difference.Expected+", actual "+difference.Actual)
			}
			item.Failure = &junitFailure{Message: "contract difference", Body: strings.Join(lines, "\n")}
		}
		suite.Cases = append(suite.Cases, item)
	}
	payload, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	payload = append([]byte(xml.Header), payload...)
	payload = append(payload, '\n')
	if err := validateEvidence(payload); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}
