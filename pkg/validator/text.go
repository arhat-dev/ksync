package validator

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"arhat.dev/pkg/log"
	"arhat.dev/pkg/textquery"
	"github.com/itchyny/gojq"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/yaml"
)

func init() {
	RegisterValidator(MethodText, NewTextValidator)
}

const (
	MethodText = "text"
)

const (
	TextSchemaPlainText = "plaintext"
	TextSchemaJSON      = "json"
	TextSchemaYAML      = "yaml"
)

type TextConfig struct {
	QueryResultAsData bool `json:"queryResultAsData" yaml:"queryResultAsData"`

	// Query in jq format, no template support
	Query string `json:"query" yaml:"query"`
	// Variables used when executing the query (with template support)
	Variables NameValuePairs `json:"variables" yaml:"variables"`

	Expect struct {
		// Schema expectation for query result (no template support)
		Schema string `json:"schema" yaml:"schema"`
		// Data content expectation for query result (with template support)
		Data string `json:"data" yaml:"data"`
	}
}

func NewTextValidator(ctx context.Context, logger log.Interface, config *Config) (Interface, error) {
	if config.Text == nil {
		return nil, fmt.Errorf("no text validator configuration provided")
	}

	q, err := gojq.Parse(config.Text.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query %q: %w", config.Text.Query, err)
	}

	variables, err := config.Text.Variables.ToNameValueTemplatePairs()
	if err != nil {
		return nil, fmt.Errorf("failed to parse variables as template: %w", err)
	}

	var expectDataTpl *template.Template
	if config.Text.Expect.Data != "" {
		expectDataTpl, err = template.New("").Funcs(funcMapWithJQ()).Parse(config.Text.Expect.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse expect data as template: %w", err)
		}
	}

	return &TextValidator{
		ctx:      ctx,
		dataKeys: config.DataKeys,

		query:     q,
		variables: variables,

		queryResultAsData: config.Text.QueryResultAsData,

		expectSchema:  config.Text.Expect.Schema,
		expectDataTpl: expectDataTpl,
	}, nil
}

type TextValidator struct {
	ctx      context.Context
	dataKeys []string

	query     *gojq.Query
	variables NameValueTemplatePairs

	queryResultAsData bool

	expectSchema  string
	expectDataTpl *template.Template
}

func (j *TextValidator) Validate(data map[string][]byte) *DataMsg {
	result := &DataMsg{
		Data:   make(map[string][]byte),
		Errors: make(map[string]error),
	}
	for _, k := range j.dataKeys {
		d, ok := data[k]
		if !ok {
			continue
		}

		tplVar := &templateVars{
			DataKeys: j.dataKeys,
			DataKey:  k,
			Data:     d,
		}

		vm, err := j.variables.EvalAndConvertToStringInterfacesMap(tplVar)
		if err != nil {
			result.Errors[k] = fmt.Errorf("failed to eval variables: %w", err)
			continue
		}

		variables := make(map[string]interface{})
		for k, vs := range vm {
			if len(vs) != 0 {
				variables[k] = vs[0]
			}
		}

		var (
			unmarshal func([]byte, interface{}) error
			dataInput interface{}
		)

		switch j.expectSchema {
		case TextSchemaJSON:
			unmarshal = json.Unmarshal
		case TextSchemaYAML:
			unmarshal = func(bytes []byte, i interface{}) error {
				return yaml.UnmarshalStrict(bytes, i)
			}
		case "", TextSchemaPlainText:
			dataInput = string(d)
		}

		if unmarshal != nil {
			dataInput = make(map[string]interface{})
			err = unmarshal(d, &dataInput)
			if err != nil {
				// maybe it's an array
				dataInput = []interface{}{}
				err = unmarshal(d, &dataInput)
			}

			if err != nil {
				result.Errors[k] = fmt.Errorf("%s shcema not valid: %w", j.expectSchema, err)
				continue
			}
		}

		queryRet, found, err := textquery.RunQuery(j.query, dataInput, variables)
		if err != nil {
			result.Errors[k] = fmt.Errorf("query failed: %w", err)
			continue
		}

		if !found {
			result.Errors[k] = fmt.Errorf("no result found for query")
			continue
		}

		queryResult := []byte(queryRet)

		if j.expectDataTpl != nil {
			buf := new(bytes.Buffer)
			err = j.expectDataTpl.Execute(buf, tplVar)
			if err != nil {
				result.Errors[k] = fmt.Errorf("failed to execute expect data template: %w", err)
				continue
			}

			if !bytes.Equal(queryResult, buf.Bytes()) {
				result.Errors[k] = fmt.Errorf("query result unexpected")
				continue
			}
		}

		if j.queryResultAsData {
			// nolint:gocritic
			switch j.expectSchema {
			case TextSchemaYAML:
				queryResult, err = yaml.JSONToYAML(queryResult)
				if err != nil {
					result.Errors[k] = fmt.Errorf("failed to convert query result to json: %w", err)
					continue
				}
			}
			result.Data[k] = queryResult
		} else {
			result.Data[k] = d
		}
	}

	return result
}
