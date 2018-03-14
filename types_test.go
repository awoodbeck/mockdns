package main

import (
	"encoding/json"
	"io/ioutil"
	"testing"
)

func TestDataUnmarshal(t *testing.T) {
	t.Parallel()

	b, err := ioutil.ReadFile("example.json")
	if err != nil {
		t.Fatal(err)
	}

	d := make(data)
	err = json.Unmarshal(b, &d)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDataUnmarshalNilData(t *testing.T) {
	t.Parallel()

	var d data
	b := []byte(`{"test.com.": {}}`)
	err := json.Unmarshal(b, &d)
	if err != errNilMapUnmarshal {
		t.Fatalf("expected nil map error; actual: %s", err)
	}
}

func TestDataUnmarshalNilRecs(t *testing.T) {
	t.Parallel()

	b := []byte(`{"test.com.": {}}`)
	d := make(data)
	err := json.Unmarshal(b, &d)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNilRRFromNilMap(t *testing.T) {
	var m map[string]string
	var recs records

	rr, err := recs.rrFromMap("A", "test.com.", m)
	if rr != nil || err != nil {
		t.Fatalf("expected nil, nil; actual: %v, %s", rr, err)
	}
}
