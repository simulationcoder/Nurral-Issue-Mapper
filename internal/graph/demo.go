package graph

import (
	_ "embed"
	"encoding/json"
)

//go:embed testdata/demo.json
var demoJSON []byte

// NewDemoService serves an invented tracker ("acme/shop") with no token and no
// network, so the page and the reports can be tried before connecting a repo.
func NewDemoService() *Service {
	var g Graph
	if err := json.Unmarshal(demoJSON, &g); err != nil {
		panic("graph: bad embedded demo.json: " + err.Error())
	}
	return &Service{demo: &g, Rules: DefaultRules()}
}
