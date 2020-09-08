package validator

import (
	"bytes"
	"fmt"
	"text/template"
)

type NameValuePairs []NameValuePair

func (p NameValuePairs) ToNameValueTemplatePairs() (NameValueTemplatePairs, error) {
	var result NameValueTemplatePairs
	for _, p := range p {
		tp, err := p.ToNameValueTemplatePair()
		if err != nil {
			return nil, err
		}
		result = append(result, tp)
	}

	return result, nil
}

type NameValuePair struct {
	Name  string      `json:"name" yaml:"name"`
	Value interface{} `json:"value" yaml:"value"`
}

func (p NameValuePair) ToNameValueTemplatePair() (*NameValueTemplatePair, error) {
	return createNameValueTemplatePair(p.Name, p.Value)
}

func createNameValueTemplatePair(nameTpl string, valueTpl interface{}) (*NameValueTemplatePair, error) {
	nameT, err := template.New("").Funcs(funcMapWithJQ()).Parse(nameTpl)
	if err != nil {
		return nil, err
	}

	var valueT *template.Template
	if valueTplStr, ok := valueTpl.(string); ok {
		valueT, err = template.New("").Funcs(funcMapWithJQ()).Parse(valueTplStr)
		if err != nil {
			return nil, err
		}
	}

	return &NameValueTemplatePair{
		nameTpl:  nameT,
		valueTpl: valueT,
		value:    valueTpl,
	}, nil
}

type NameValueTemplatePairs []*NameValueTemplatePair

func (pairs NameValueTemplatePairs) EvalAndConvertToStringInterfacesMap(
	data interface{},
) (map[string][]interface{}, error) {
	result := make(map[string][]interface{})
	for _, p := range pairs {
		name, value, err := p.Eval(data)
		if err != nil {
			return nil, err
		}

		if m, ok := result[name]; !ok || m == nil {
			result[name] = []interface{}{}
		}

		result[name] = append(result[name], value)
	}

	return result, nil
}

func (pairs NameValueTemplatePairs) EvalAndConvertToStringStringsMap(data interface{}) (map[string][]string, error) {
	result := make(map[string][]string)
	for _, p := range pairs {
		name, value, err := p.Eval(data)
		if err != nil {
			return nil, err
		}

		if m, ok := result[name]; !ok || m == nil {
			result[name] = []string{}
		}

		if vStr, ok := value.(string); ok {
			result[name] = append(result[name], vStr)
		} else {
			result[name] = append(result[name], fmt.Sprint(value))
		}
	}

	return result, nil
}

type NameValueTemplatePair struct {
	nameTpl  *template.Template
	valueTpl *template.Template
	value    interface{}
}

func (h NameValueTemplatePair) Eval(data interface{}) (name string, value interface{}, err error) {
	buf := new(bytes.Buffer)
	err = h.nameTpl.Execute(buf, data)
	if err != nil {
		return
	}

	name = buf.String()
	buf.Reset()
	if h.valueTpl != nil {
		err = h.valueTpl.Execute(buf, data)
		if err != nil {
			return
		}

		value = buf.String()
	} else {
		value = h.value
	}

	return
}
