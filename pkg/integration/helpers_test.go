package integration_test

import (
	"os"
	"testing"
)

const (
	hooverImg = "../../testdata/synthetic/HOOVER.img"
	stabImg   = "../../testdata/synthetic/STAB.img"
	technoImg = "../../testdata/synthetic/TECHNO.img"
	brassImg  = "../../testdata/synthetic/BRASS.img"
	padLFOImg = "../../testdata/synthetic/PAD-LFO.img"
)

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}

func requireFullCorpus(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(corpusRoot)
	if err != nil || len(entries) < 3 {
		t.Fatal("full hardware corpus is not installed; run `make corpus`")
	}
}
