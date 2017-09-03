// Copyright 2017 Google Inc. All rights reserved.
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

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"android/soong/jar"
	"android/soong/third_party/zip"
)

var (
	sortEntries = flag.Bool("s", false, "sort entries (defaults to the order from the input zip files)")
	sortJava    = flag.Bool("j", false, "sort zip entries using jar ordering (META-INF first)")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: merge_zips [-j] output [inputs...]")
		flag.PrintDefaults()
	}

	// parse args
	flag.Parse()
	args := flag.Args()
	if len(args) < 2 {
		flag.Usage()
		os.Exit(1)
	}
	outputPath := args[0]
	inputs := args[1:]

	log.SetFlags(log.Lshortfile)

	// make writer
	output, err := os.Create(outputPath)
	if err != nil {
		log.Fatal(err)
	}
	defer output.Close()
	writer := zip.NewWriter(output)
	defer func() {
		err := writer.Close()
		if err != nil {
			log.Fatal(err)
		}
	}()

	// make readers
	readers := []namedZipReader{}
	for _, input := range inputs {
		reader, err := zip.OpenReader(input)
		if err != nil {
			log.Fatal(err)
		}
		defer reader.Close()
		namedReader := namedZipReader{path: input, reader: reader}
		readers = append(readers, namedReader)
	}

	// do merge
	if err := mergeZips(readers, writer, *sortEntries, *sortJava); err != nil {
		log.Fatal(err)
	}
}

// a namedZipReader reads a .zip file and can say which file it's reading
type namedZipReader struct {
	path   string
	reader *zip.ReadCloser
}

// a zipEntryPath refers to a file contained in a zip
type zipEntryPath struct {
	zipName   string
	entryName string
}

func (p zipEntryPath) String() string {
	return p.zipName + "/" + p.entryName
}

// a zipEntry knows the location and content of a file within a zip
type zipEntry struct {
	path    zipEntryPath
	content *zip.File
}

// a fileMapping specifies to copy a zip entry from one place to another
type fileMapping struct {
	source zipEntry
	dest   string
}

func mergeZips(readers []namedZipReader, writer *zip.Writer, sortEntries bool, sortJava bool) error {

	mappingsByDest := make(map[string]fileMapping, 0)
	orderedMappings := []fileMapping{}

	for _, namedReader := range readers {
		for _, file := range namedReader.reader.File {
			// check for other files or directories destined for the same path
			dest := file.Name
			mapKey := dest
			if strings.HasSuffix(mapKey, "/") {
				mapKey = mapKey[:len(mapKey)-1]
			}
			existingMapping, exists := mappingsByDest[mapKey]

			// make a new entry to add
			source := zipEntry{path: zipEntryPath{zipName: namedReader.path, entryName: file.Name}, content: file}
			newMapping := fileMapping{source: source, dest: dest}

			// handle duplicates
			if exists {
				wasDir := existingMapping.source.content.FileHeader.FileInfo().IsDir()
				isDir := newMapping.source.content.FileHeader.FileInfo().IsDir()
				if !wasDir || !isDir {
					return fmt.Errorf("Duplicate path %v found in %v and %v\n",
						dest, existingMapping.source.path, newMapping.source.path)
				}
			}

			// save entry
			mappingsByDest[mapKey] = newMapping
			orderedMappings = append(orderedMappings, newMapping)
		}

	}

	if sortJava {
		jarSort(orderedMappings)
	} else if sortEntries {
		alphanumericSort(orderedMappings)
	}

	for _, entry := range orderedMappings {
		if err := writer.CopyFrom(entry.source.content, entry.dest); err != nil {
			return err
		}
	}

	return nil
}

func jarSort(files []fileMapping) {
	sort.SliceStable(files, func(i, j int) bool {
		return jar.EntryNamesLess(files[i].dest, files[j].dest)
	})
}

func alphanumericSort(files []fileMapping) {
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].dest < files[j].dest
	})
}
