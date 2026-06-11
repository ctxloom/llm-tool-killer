package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPrintVersionTextEmitsBareVersion(t *testing.T) {
	var buf bytes.Buffer
	if err := printVersion(&buf, "text"); err != nil {
		t.Fatalf("printVersion: %v", err)
	}
	if got, want := buf.String(), Version+"\n"; got != want {
		t.Fatalf("text output = %q, want %q", got, want)
	}
}

func TestPrintVersionJSONEmitsNameAndVersion(t *testing.T) {
	var buf bytes.Buffer
	if err := printVersion(&buf, "json"); err != nil {
		t.Fatalf("printVersion: %v", err)
	}
	var got versionInfo
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	if got.Name != progName || got.Version != Version {
		t.Fatalf("json output = %+v, want {Name: %q, Version: %q}", got, progName, Version)
	}
}

func TestPrintVersionUnknownFormatErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := printVersion(&buf, "yaml"); err == nil {
		t.Fatal("want error for unknown format")
	}
	if buf.Len() != 0 {
		t.Fatalf("unknown format must not write output, got %q", buf.String())
	}
}
