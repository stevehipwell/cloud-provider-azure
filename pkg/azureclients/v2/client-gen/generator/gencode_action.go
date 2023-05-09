// /*
// Copyright The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

// Package generator
package generator

import (
	"bufio"
	"bytes"
	"fmt"
	"go/format"
	"io"
	"os/exec"
	"strings"

	"sigs.k8s.io/controller-tools/pkg/genall"
	"sigs.k8s.io/controller-tools/pkg/loader"
	"sigs.k8s.io/controller-tools/pkg/markers"
)

// generateClient writes out the build tag, package declaration, and imports
func generateClient(ctx *genall.GenerationContext, root *loader.Package, headerText string) error {
	var importList = make(map[string]map[string]struct{})
	var codeSnips []*bytes.Buffer
	err := markers.EachType(ctx.Collector, root, func(typeInfo *markers.TypeInfo) {
		if typeInfo := typeInfo.Markers.Get(clientGenMarker.Name); typeInfo != nil {
			markerConf := typeInfo.(ClientGenConfig)
			fmt.Printf("found marker,%+v", markerConf)
			//nolint:gosec // G204 ignore this!
			if err := exec.Command("go", "get", markerConf.PackageName).Run(); err != nil {
				root.AddError(err)
				return
			}

			var outContent bytes.Buffer
			if err := ClientTemplate.Execute(&outContent, markerConf); err != nil {
				root.AddError(err)
			}
			//context.Context
			importList["context"] = make(map[string]struct{})
			//utils.Funcs
			importList["sigs.k8s.io/cloud-provider-azure/pkg/azureclients/v2/utils"] = make(map[string]struct{})

			if err := ClientFactoryTemplate.Execute(&outContent, markerConf); err != nil {
				root.AddError(err)
			}
			//	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
			importList["github.com/Azure/azure-sdk-for-go/sdk/azcore"] = make(map[string]struct{})

			//"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
			importList["github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"] = make(map[string]struct{})

			// define structs
			for _, verb := range markerConf.Verbs {
				switch true {
				case strings.EqualFold(FuncCreateOrUpdate, verb):
					if err := CreateOrUpdateFuncTemplate.Execute(&outContent, markerConf); err != nil {
						root.AddError(err)
					}
				case strings.EqualFold(FuncDelete, verb):
					if err := DeleteFuncTemplate.Execute(&outContent, markerConf); err != nil {
						root.AddError(err)
					}
				case strings.EqualFold(FuncListByRG, verb):
					if err := ListByRGFuncTemplate.Execute(&outContent, markerConf); err != nil {
						root.AddError(err)
					}
				case strings.EqualFold(FuncList, verb):
					if err := ListFuncTemplate.Execute(&outContent, markerConf); err != nil {
						root.AddError(err)
					}
				case strings.EqualFold(FuncGet, verb):
					if err := GetFuncTemplate.Execute(&outContent, markerConf); err != nil {
						root.AddError(err)
					}
				}
			}

			aliasMap, ok := importList[markerConf.PackageName]
			if !ok {
				aliasMap = make(map[string]struct{})
				importList[markerConf.PackageName] = aliasMap
			}

			aliasMap[markerConf.PackageAlias] = struct{}{}

			codeSnips = append(codeSnips, &outContent)
		}

	})
	if err != nil {
		root.AddError(err)
		return err
	}
	if len(codeSnips) <= 0 {
		return nil
	}

	outContent := new(bytes.Buffer)

	var importStatement bytes.Buffer
	importWriter := bufio.NewWriter(&importStatement)
	for packageName, alias := range importList {
		if len(alias) == 0 {
			if err := ImportTemplate.Execute(importWriter, &ImportStatement{Alias: "", Package: packageName}); err != nil {
				return err
			}
		}
		for item := range alias {
			if err := ImportTemplate.Execute(importWriter, &ImportStatement{Alias: item, Package: packageName}); err != nil {
				return err
			}
		}
	}
	importWriter.Flush()
	_, err = fmt.Fprintf(outContent, `
%[3]s
// Code generated by client-gen. DO NOT EDIT.
package %[1]s
import (
%[2]s
)
`, root.Name, importStatement.String(), headerText)
	if err != nil {
		return err
	}

	for _, codeSnip := range codeSnips {
		if _, err := io.Copy(outContent, bufio.NewReader(codeSnip)); err != nil {
			return err
		}
	}
	fmt.Println(string(outContent.Bytes()))
	formattedBytes, err := format.Source(outContent.Bytes())
	if err != nil {
		return err
		// we still write the invalid source to disk to figure out what went wrong
	}

	outputFile, err := ctx.Open(root, "zz_generated.client.go")
	if err != nil {
		return err
	}
	defer outputFile.Close()
	n, err := outputFile.Write(formattedBytes)
	if err != nil {
		return err
	}
	if n < len(formattedBytes) {
		return io.ErrShortWrite
	}
	return nil
}