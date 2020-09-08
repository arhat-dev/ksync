package validator

import (
	"text/template"

	"arhat.dev/pkg/textquery"
	"github.com/Masterminds/sprig/v3"
)

func funcMapWithJQ() template.FuncMap {
	fm := sprig.HermeticTxtFuncMap()
	fm["jq"] = textquery.JQ
	fm["jqBytes"] = textquery.JQBytes
	return fm
}
