// Package configs 内嵌默认配置模板与默认人格档案。
package configs

import _ "embed"

// ConfigTemplate 是 buugoo init 使用的配置模板，其中 {{DATA_DIR}}、{{JWT_SECRET}}、{{MODE}}、
// {{LISTEN}} 会在生成时替换。
//
//go:embed config.example.yaml
var ConfigTemplate string

// DefaultPersona 是内置的默认人格档案（布谷）。
//
//go:embed persona.default.yaml
var DefaultPersona []byte
