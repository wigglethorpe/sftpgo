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

//go:build bundle
// +build bundle

package httpd

import (
	"github.com/go-chi/chi/v5"

	"github.com/drakkan/sftpgo/v2/internal/bundle"
)

func serveStaticDir(router chi.Router, path, _ string) {
	switch path {
	case webStaticFilesPath:
		fileServer(router, path, bundle.GetStaticFs())
	case webOpenAPIPath:
		fileServer(router, path, bundle.GetOpenAPIFs())
	}
}
