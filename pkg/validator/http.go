package validator

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	pkgurl "net/url"
	"strings"
	"text/template"
	"time"

	"arhat.dev/pkg/confhelper"
	"arhat.dev/pkg/log"
	"golang.org/x/net/http/httpproxy"
)

func init() {
	RegisterValidator(MethodHTTP, NewHTTPValidator)
}

const (
	MethodHTTP = "http"
)

type HTTPProxyConfig struct {
	HTTP    string `json:"http" yaml:"http"`
	HTTPS   string `json:"https" yaml:"https"`
	NoProxy string `json:"noProxy" yaml:"noProxy"`
	CGI     bool   `json:"cgi" yaml:"cgi"`
}

type HTTPConfig struct {
	DryRun             bool `json:"dryRun" yaml:"dryRun"`
	RequestBodyAsData  bool `json:"requestBodyAsData" yaml:"requestBodyAsData"`
	ResponseBodyAsData bool `json:"responseBodyAsData" yaml:"responseBodyAsData"`

	Request struct {
		URLTemplate string               `json:"url" yaml:"url"`
		Action      string               `json:"action" yaml:"action"`
		Headers     NameValuePairs       `json:"headers" yaml:"headers"`
		Proxy       *HTTPProxyConfig     `json:"proxy" yaml:"proxy"`
		Body        string               `json:"body" yaml:"body"`
		TLS         confhelper.TLSConfig `json:"tls" yaml:"tls"`
	} `json:"request" yaml:"request"`

	Response struct {
		Body string `json:"body" yaml:"body"`
	} `json:"response" yaml:"response"`

	Expect struct {
		ResponseCode    int            `json:"responseCode" yaml:"responseCode"`
		ResponseBody    string         `json:"responseBody" yaml:"responseBody"`
		ResponseHeaders NameValuePairs `json:"responseHeaders" yaml:"responseHeaders"`
	} `json:"expect" yaml:"expect"`
}

func NewHTTPValidator(context context.Context, logger log.Interface, config *Config) (Interface, error) {
	if config.HTTP == nil {
		return nil, fmt.Errorf("no http validator configuration provided")
	}

	if config.Method != MethodHTTP {
		return nil, fmt.Errorf("validator method %q is not supported, expect %q", config.Method, MethodHTTP)
	}

	if config.HTTP.RequestBodyAsData && config.HTTP.ResponseBodyAsData {
		return nil, fmt.Errorf("only one of the request body or response body can be used as data, not both")
	}

	method := strings.ToUpper(config.HTTP.Request.Action)
	_, ok := map[string]struct{}{
		http.MethodGet:     {},
		http.MethodPost:    {},
		http.MethodPut:     {},
		http.MethodHead:    {},
		http.MethodOptions: {},
	}[method]
	if !ok {
		return nil, fmt.Errorf("unsupported http action method %q", config.HTTP.Request.Action)
	}

	var (
		reqBodyTpl  *template.Template
		respBodyTpl *template.Template
		urlTpl, err = template.New("").Funcs(funcMapWithJQ()).Parse(config.HTTP.Request.URLTemplate)
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url as text template: %w", err)
	}

	if config.HTTP.Request.Body != "" {
		if method == http.MethodGet {
			return nil, fmt.Errorf("http body is not allowed in action method GET")
		}

		reqBodyTpl, err = template.New("").Funcs(funcMapWithJQ()).Parse(config.HTTP.Request.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to parse request body as text template: %w", err)
		}
	}

	if config.HTTP.Response.Body != "" {
		respBodyTpl, err = template.New("").Funcs(funcMapWithJQ()).Parse(config.HTTP.Response.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response body as text template: %w", err)
		}
	}

	reqHeaders, err := config.HTTP.Request.Headers.ToNameValueTemplatePairs()
	if err != nil {
		return nil, fmt.Errorf("failed to parse header as text template: %w", err)
	}

	expHeaders, err := config.HTTP.Expect.ResponseHeaders.ToNameValueTemplatePairs()
	if err != nil {
		return nil, fmt.Errorf("failed to parse expected headers as text template: %w", err)
	}

	var proxy func(*http.Request) (*pkgurl.URL, error)
	if p := config.HTTP.Request.Proxy; p != nil {
		cfg := httpproxy.Config{
			HTTPProxy:  p.HTTP,
			HTTPSProxy: p.HTTPS,
			NoProxy:    p.NoProxy,
			CGI:        p.CGI,
		}

		pf := cfg.ProxyFunc()

		proxy = func(req *http.Request) (*pkgurl.URL, error) {
			return pf(req.URL)
		}
	}

	tlsConfig, err := config.HTTP.Request.TLS.GetTLSConfig(false)
	if err != nil {
		return nil, fmt.Errorf("failed to load tls config: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			// TODO: make it fully customizable?
			Proxy: proxy,
			DialContext: (&net.Dialer{
				Timeout:       30 * time.Second,
				KeepAlive:     30 * time.Second,
				FallbackDelay: 300 * time.Millisecond,
			}).DialContext,
			ForceAttemptHTTP2:     tlsConfig != nil,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       tlsConfig,

			DialTLSContext:         nil,
			DisableKeepAlives:      false,
			DisableCompression:     false,
			MaxConnsPerHost:        0,
			ResponseHeaderTimeout:  0,
			TLSNextProto:           nil,
			ProxyConnectHeader:     nil,
			MaxResponseHeaderBytes: 0,
			WriteBufferSize:        0,
			ReadBufferSize:         0,
		},
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}

	v := &HTTPValidator{
		logger:   logger,
		ctx:      context,
		dataKeys: append([]string{}, config.DataKeys...),

		dryRun: config.HTTP.DryRun,
		client: client,

		method:        method,
		urlTpl:        urlTpl,
		reqBodyTpl:    reqBodyTpl,
		headers:       reqHeaders,
		reqBodyAsData: config.HTTP.RequestBodyAsData,

		respBodyTpl:    respBodyTpl,
		respBodyAsData: config.HTTP.ResponseBodyAsData,

		expectResponseCode:    config.HTTP.Expect.ResponseCode,
		expectResponseBody:    config.HTTP.Expect.ResponseBody,
		expectResponseHeaders: expHeaders,
	}

	return v, nil
}

// nolint:maligned
type HTTPValidator struct {
	logger   log.Interface
	ctx      context.Context
	dataKeys []string

	dryRun bool
	client *http.Client

	// request
	method        string
	urlTpl        *template.Template
	reqBodyTpl    *template.Template
	headers       NameValueTemplatePairs
	reqBodyAsData bool

	// response
	respBodyTpl    *template.Template
	respBodyAsData bool

	// response validation
	expectResponseCode    int
	expectResponseBody    string
	expectResponseHeaders NameValueTemplatePairs
}

func (h *HTTPValidator) Validate(data map[string][]byte) *DataMsg {
	if len(data) == 0 {
		return nil
	}

	result := &DataMsg{
		Data:   make(map[string][]byte),
		Errors: make(map[string]error),
	}

	h.logger.V("validating started")
	for _, k := range h.dataKeys {
		d, ok := data[k]
		if !ok {
			continue
		}

		tplVars := &templateVars{
			DataKeys: h.dataKeys,
			DataKey:  k,
			Data:     d,
		}

		h.logger.V("creating request")
		req, reqBody, err := h.newRequest(tplVars)
		if err != nil {
			result.Errors[k] = fmt.Errorf("failed to create validation request: %w", err)
			continue
		}

		var respBody []byte
		if !h.dryRun {
			h.logger.V("sending request")
			resp, err := h.client.Do(req)
			if err != nil {
				result.Errors[k] = fmt.Errorf("failed to do validation request for key %q: %w", k, err)
				continue
			}

			h.logger.V("handling response")
			respBody, err = h.handleResponse(tplVars, resp)
			if err != nil {
				result.Errors[k] = fmt.Errorf("data for key %q not valid: %w", k, err)
				continue
			}
		}

		switch {
		case h.reqBodyAsData:
			result.Data[k] = reqBody
		case h.respBodyAsData:
			result.Data[k] = respBody
		default:
			result.Data[k] = data[k]
		}
	}

	return result
}

func (h *HTTPValidator) newRequest(tplVars *templateVars) (*http.Request, []byte, error) {
	var requestBody []byte

	buf := new(bytes.Buffer)
	err := h.urlTpl.Execute(buf, tplVars)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute url template: %w", err)
	}
	url := buf.String()

	if h.reqBodyTpl != nil {
		buf.Reset()
		err = h.reqBodyTpl.Execute(buf, tplVars)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to execute request body template: %w", err)
		}

		requestBody = buf.Bytes()
	}

	req, err := http.NewRequestWithContext(h.ctx, h.method, url, buf)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request ")
	}

	headers, err := h.headers.EvalAndConvertToStringStringsMap(tplVars)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute header name value templates: %w", err)
	}

	if req.Header == nil {
		// set header if no default header
		if len(headers) != 0 {
			req.Header = headers
		}
	} else {
		// override if has default header
		for k := range headers {
			req.Header[k] = headers[k]
		}
	}

	return req, requestBody, nil
}

func (h *HTTPValidator) handleResponse(tplVar *templateVars, resp *http.Response) ([]byte, error) {
	type responseVars struct {
		Resp struct {
			Body []byte
		}
	}

	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	var (
		respBody []byte
		err      error
	)

	if resp.Body != nil {
		respBody, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
	}

	if h.expectResponseCode != 0 {
		if resp.StatusCode != h.expectResponseCode {
			return nil, fmt.Errorf("response code expected %d, but got %d", h.expectResponseCode, resp.StatusCode)
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// no response code specified, use 2xx
		return nil, fmt.Errorf("response code %d not in range 2xx", resp.StatusCode)
	}

	if h.respBodyTpl != nil {
		buf := new(bytes.Buffer)
		err = h.respBodyTpl.Execute(buf, &templateVars{
			DataKeys: tplVar.DataKeys,
			DataKey:  tplVar.DataKey,
			Data:     tplVar.Data,
			Extra: &responseVars{
				Resp: struct{ Body []byte }{Body: respBody},
			},
		})

		if err != nil {
			return nil, fmt.Errorf("failed to execute response body template: %w", err)
		}

		respBody = buf.Bytes()
	}

	if h.expectResponseBody != "" {
		if h.expectResponseBody != string(respBody) {
			return nil, fmt.Errorf("response body not valid: %w", err)
		}
	}

	if len(h.expectResponseHeaders) != 0 {
		expHeaders, err := h.expectResponseHeaders.EvalAndConvertToStringStringsMap(&templateVars{
			DataKeys: tplVar.DataKeys,
			DataKey:  tplVar.DataKey,
			Data:     tplVar.Data,
			Extra: &responseVars{
				Resp: struct{ Body []byte }{Body: respBody},
			},
		})

		if err != nil {
			return nil, fmt.Errorf("failed to eval expected headers: %w", err)
		}

		for k, strList := range expHeaders {
			s, ok := resp.Header[k]
			if !ok {
				return nil, fmt.Errorf("header not valid, missing %q", k)
			}

			for _, v := range strList {
				found := false
				for _, sv := range s {
					if sv == v {
						found = true
						break
					}
				}

				if !found {
					return nil, fmt.Errorf("header %q not valid, missing value %q", k, v)
				}
			}
		}
	}

	return respBody, nil
}
