// Copyright (C) 2019-2022  Nicola Murino
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

//go:build !bundle
// +build !bundle

package util

import (
	"html/template"
	"os"
	"path/filepath"
	"runtime"

	"github.com/drakkan/sftpgo/v2/internal/logger"
)

// FindSharedDataPath searches for the specified directory name in searchDir
// and in system-wide shared data directories.
// If name is an absolute path it is returned unmodified.
func FindSharedDataPath(name, searchDir string) string {
	if !IsFileInputValid(name) {
		return ""
	}
	if name != "" && !filepath.IsAbs(name) {
		searchList := []string{searchDir}
		if additionalSharedDataSearchPath != "" {
			searchList = append(searchList, additionalSharedDataSearchPath)
		}
		if runtime.GOOS != osWindows {
			searchList = append(searchList, "/usr/share/sftpgo")
			searchList = append(searchList, "/usr/local/share/sftpgo")
		}
		searchList = RemoveDuplicates(searchList, false)
		for _, basePath := range searchList {
			res := filepath.Join(basePath, name)
			_, err := os.Stat(res)
			if err == nil {
				logger.Debug(logSender, "", "found share data path for name %#v: %#v", name, res)
				return res
			}
		}
		return filepath.Join(searchDir, name)
	}
	return name
}

// LoadTemplate parses the given template paths.
// It behaves like template.Must but it writes a log before exiting.
// You can optionally provide a base template (e.g. to define some custom functions)
func LoadTemplate(base *template.Template, paths ...string) *template.Template {
	var t *template.Template
	var err error

	if base != nil {
		base, err = base.Clone()
		if err == nil {
			t, err = base.ParseFiles(paths...)
		}
	} else {
		t, err = template.ParseFiles(paths...)
	}

	if err != nil {
		logger.ErrorToConsole("error loading required template: %v", err)
		logger.Error(logSender, "", "error loading required template: %v", err)
		panic(err)
	}
	return t
}
