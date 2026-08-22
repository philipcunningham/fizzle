package webcore

import (
	"encoding/json"
	"os"
	"testing"
)

// panelMeasurement is one row of testdata/panel-measurements.json: what
// the FZ front panel shows for a schema field, and how it maps to the
// stored byte.
type panelMeasurement struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	PanelMin     int    `json:"panelMin"`
	PanelMax     int    `json:"panelMax"`
	PanelOptions int    `json:"panelOptions"`
	Rule         string `json:"rule"`
}

type panelMeasurements struct {
	Source   string             `json:"source"`
	Measured string             `json:"measured"`
	Fields   []panelMeasurement `json:"fields"`
}

func loadPanelMeasurements(t *testing.T) panelMeasurements {
	t.Helper()
	raw, err := os.ReadFile("testdata/panel-measurements.json")
	if err != nil {
		t.Fatalf("read measurements: %v", err)
	}
	var m panelMeasurements
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse measurements: %v", err)
	}
	return m
}

// A schema field with no measurement is a field nobody checked against
// the machine, and a measurement with no field is a stale row.
func TestSchemaAndMeasurementsCoverEachOther(t *testing.T) {
	measured := map[string]bool{}
	for _, m := range loadPanelMeasurements(t).Fields {
		measured[m.ID] = true
	}
	inSchema := map[string]bool{}
	for _, f := range Schema() {
		inSchema[f.ID] = true
		if !measured[f.ID] {
			t.Errorf("schema field %q has no row in testdata/panel-measurements.json", f.ID)
		}
	}
	for id := range measured {
		if !inSchema[id] {
			t.Errorf("measurement %q names no schema field", id)
		}
	}
}

// The range the UI offers has to be the range the panel offers.
func TestSchemaRangesMatchThePanel(t *testing.T) {
	byID := map[string]panelMeasurement{}
	for _, m := range loadPanelMeasurements(t).Fields {
		byID[m.ID] = m
	}
	for _, f := range Schema() {
		m, ok := byID[f.ID]
		if !ok {
			continue
		}
		if f.Kind == kindSelect {
			if m.PanelOptions != len(f.Options) {
				t.Errorf("%s: schema offers %d options, the panel offers %d",
					f.ID, len(f.Options), m.PanelOptions)
			}
			continue
		}
		if f.Min != m.PanelMin || f.Max != m.PanelMax {
			t.Errorf("%s: schema range %d to %d, panel range %d to %d (%s)",
				f.ID, f.Min, f.Max, m.PanelMin, m.PanelMax, m.Rule)
		}
	}
}

// The measurements came off an emulator, and the file has to keep
// saying so. A row quoted elsewhere loses that qualifier fast.
func TestMeasurementsRecordTheirSource(t *testing.T) {
	m := loadPanelMeasurements(t)
	if m.Source != "emulator" {
		t.Errorf("source = %q, want emulator until a device reading replaces it", m.Source)
	}
	if m.Measured == "" {
		t.Error("the measurements carry no date")
	}
}
