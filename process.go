package appdeploy

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"strings"
	"sync"
	"text/template"

	"gopkg.in/yaml.v1"
)

type Manifest struct {
	Kind     string
	Metadata Metadata
}

func (m Manifest) Filename(folder string) string {
	name := fmt.Sprintf("%s--%s.yaml", strings.ToLower(m.Kind), m.Metadata.Name)
	if folder != "" {
		return path.Join(folder, name)
	} else {
		return name
	}
}

type Metadata struct {
	Name string
}

func Process(src ManifestSource, opts Options) error {
	names, err := src.Names()
	if err != nil {
		return err
	}

	var target Target

	switch opts.Mode {
	case WriteToFolder:
		if opts.OutputFolder == "" {
			return errors.New("No output folder specified")
		}

		target = NewFolderTarget(opts.OutputFolder)
	}

	err = target.Prepare()
	if err != nil {
		return err
	}

	seen := make([]Manifest, 0)
	wg := sync.WaitGroup{}
	wg.Add(len(names))

	// Apply all resources in parallel
	for _, name := range names {
		n := name
		go func() {
			defer wg.Done()
			m, e := process(src, n, target)
			if e != nil {
				err = e
			}
			if m != nil {
				seen = append(seen, *m)
			}
		}()
	}

	wg.Wait()
	if err != nil {
		return err
	}

	err = target.Cleanup(seen)
	if err != nil {
		return err
	}

	return nil
}

func process(src ManifestSource, name string, target Target) (*Manifest, error) {
	m, err := src.Get(name)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	// Read and parse template
	data, err := ioutil.ReadAll(m)
	if err != nil {
		return nil, err
	}

	tpl, err := template.New(name).Parse(string(data))
	if err != nil {
		return nil, err
	}

	// Execute it
	var buf bytes.Buffer
	err = tpl.Execute(&buf, nil)
	if err != nil {
		return nil, err
	}

	data = bytes.TrimSpace(buf.Bytes())
	if string(data) == "" {
		// Nothing here (entire manifest in a false if-block?
		return nil, nil
	}

	// Determine object type
	var manifest Manifest
	err = yaml.Unmarshal(data, &manifest)
	if err != nil {
		return nil, err
	}

	if manifest.Kind == "" || manifest.Metadata.Name == "" {
		return nil, fmt.Errorf("%s: missing type data, not a valid Kubernetes manifest?", name)
	}

	err = target.Apply(manifest, data)
	if err != nil {
		return nil, err
	}
	return &manifest, nil
}