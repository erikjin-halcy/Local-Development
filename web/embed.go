// Package web innehåller webbgränssnittet som bäddas in i binären.
package web

import _ "embed"

//go:embed index.html
var Index []byte
