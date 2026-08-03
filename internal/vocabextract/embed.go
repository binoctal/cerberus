package vocabextract

import _ "embed"

//go:embed extractor.mjs
var extractorSrc string

//go:embed package.json
var packageJSON string
