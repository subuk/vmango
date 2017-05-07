package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"go/format"
	"io/ioutil"
	"os"
	"text/template"
)

var CODE_TEMPLATE = template.Must(
	template.ParseFiles("src/vmango/dal/aws_instance_types.go.in"),
)

type InstanceType struct {
	Name   string  `json:"instance_type"`
	Cpus   int     `json:"vCPU"`
	Memory float32 `json:"memory"`
}

func (it *InstanceType) MemoryBytes() uint64 {
	return uint64(it.Memory * 1024 * 1024 * 1024)
}

func getInstanceTypes() ([]*InstanceType, error) {
	instanceTypes := []*InstanceType{}
	source, err := os.Open("aws_instances.json.gz")
	if err != nil {
		return nil, fmt.Errorf("failed to open source file: %s", err)
	}
	reader, err := gzip.NewReader(source)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %s", err)
	}
	rawData, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read source file: %s", err)
	}
	if err := json.Unmarshal(rawData, &instanceTypes); err != nil {
		return nil, fmt.Errorf("failed to parse json from source file: %s", err)
	}
	return instanceTypes, nil
}

func main() {
	instanceTypes, err := getInstanceTypes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse instance types: %s", err)
		os.Exit(1)
		return
	}
	var buf bytes.Buffer
	CODE_TEMPLATE.Execute(&buf, struct {
		Package       string
		InstanceTypes []*InstanceType
	}{
		Package:       "dal",
		InstanceTypes: instanceTypes,
	})
	sourceCode, err := format.Source(buf.Bytes())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to gofmt generated code: %s", err)
		os.Exit(1)
		return
	}
	fmt.Println(string(sourceCode))
}
