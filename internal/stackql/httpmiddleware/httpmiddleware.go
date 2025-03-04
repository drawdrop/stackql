package httpmiddleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/stackql/any-sdk/anysdk"
	"github.com/stackql/any-sdk/pkg/logging"
	"github.com/stackql/any-sdk/pkg/requesttranslate"
	"github.com/stackql/stackql/internal/stackql/handler"
	"github.com/stackql/stackql/internal/stackql/provider"
)

func GetAuthenticatedClient(handlerCtx handler.HandlerContext, prov provider.IProvider) (*http.Client, error) {
	return getAuthenticatedClient(handlerCtx, prov)
}

func getAuthenticatedClient(handlerCtx handler.HandlerContext, prov provider.IProvider) (*http.Client, error) {
	authCtx, authErr := handlerCtx.GetAuthContext(prov.GetProviderString())
	if authErr != nil {
		return nil, authErr
	}
	httpClient, httpClientErr := prov.Auth(authCtx, authCtx.Type, false)
	if httpClientErr != nil {
		return nil, httpClientErr
	}
	return httpClient, nil
}

//nolint:nestif,mnd // acceptable for now
func parseReponseBodyIfErroneous(response *http.Response) (string, error) {
	if response != nil {
		if response.StatusCode >= 300 {
			if response.Body != nil {
				bodyBytes, bErr := io.ReadAll(response.Body)
				if bErr != nil {
					return "", bErr
				}
				bodyStr := string(bodyBytes)
				response.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
				if len(bodyStr) > 0 {
					return fmt.Sprintf("http response status code: %d, response body: %s", response.StatusCode, bodyStr), nil
				}
			}
			return fmt.Sprintf("http response status code: %d, response body is nil", response.StatusCode), nil
		}
	}
	return "", nil
}

//nolint:nestif // acceptable for now
func parseReponseBodyIfPresent(response *http.Response) (string, error) {
	if response != nil {
		if response.Body != nil {
			bodyBytes, bErr := io.ReadAll(response.Body)
			if bErr != nil {
				return "", bErr
			}
			bodyStr := string(bodyBytes)
			response.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			if len(bodyStr) > 0 {
				return fmt.Sprintf("http response status code: %d, response body: %s", response.StatusCode, bodyStr), nil
			}
			return fmt.Sprintf("http response status code: %d, response body is nil", response.StatusCode), nil
		}
	}
	return "nil response", nil
}

func HTTPApiCallFromRequest(
	handlerCtx handler.HandlerContext,
	prov provider.IProvider,
	method anysdk.OperationStore,
	request *http.Request,
) (*http.Response, error) {
	httpClient, httpClientErr := getAuthenticatedClient(handlerCtx, prov)
	if httpClientErr != nil {
		return nil, httpClientErr
	}
	request.Header.Del("Authorization")
	requestTranslator, err := requesttranslate.NewRequestTranslator(method.GetRequestTranslateAlgorithm())
	if err != nil {
		return nil, err
	}
	translatedRequest, err := requestTranslator.Translate(request)
	if err != nil {
		return nil, err
	}
	if handlerCtx.GetRuntimeContext().HTTPLogEnabled {
		urlStr := ""
		methodStr := ""
		if translatedRequest != nil && translatedRequest.URL != nil {
			urlStr = translatedRequest.URL.String()
			methodStr = translatedRequest.Method
		}
		//nolint:errcheck // output stream
		handlerCtx.GetOutErrFile().Write([]byte(fmt.Sprintf("http request url: '%s', method: '%s'\n", urlStr, methodStr)))
		body := translatedRequest.Body
		if body != nil {
			b, bErr := io.ReadAll(body)
			if bErr != nil {
				//nolint:errcheck // output stream
				handlerCtx.GetOutErrFile().Write([]byte(fmt.Sprintf("error inpecting http request body: %s\n", bErr.Error())))
			}
			bodyStr := string(b)
			translatedRequest.Body = io.NopCloser(bytes.NewBuffer(b))
			//nolint:errcheck // output stream
			handlerCtx.GetOutErrFile().Write([]byte(fmt.Sprintf("http request body = '%s'\n", bodyStr)))
		}
	}
	walObj, _ := handlerCtx.GetTSM()
	logging.GetLogger().Debugf("Proof of invariant: walObj = %v", walObj)
	urlString := translatedRequest.URL.String()
	logging.GetLogger().Debugf("HTTP request: URL = '''%s'''", urlString)
	r, err := httpClient.Do(translatedRequest)
	responseErrorBodyToPublish, reponseParseErr := parseReponseBodyIfErroneous(r)
	if reponseParseErr != nil {
		return nil, reponseParseErr
	}
	if responseErrorBodyToPublish != "" {
		//nolint:errcheck // output stream
		handlerCtx.GetOutErrFile().Write([]byte(fmt.Sprintf("%s\n", responseErrorBodyToPublish)))
	} else if handlerCtx.GetRuntimeContext().HTTPLogEnabled {
		reponseBodyStr, _ := parseReponseBodyIfPresent(r)
		//nolint:errcheck // output stream
		handlerCtx.GetOutErrFile().Write([]byte(fmt.Sprintf("%s\n", reponseBodyStr)))
	}
	if err != nil {
		if handlerCtx.GetRuntimeContext().HTTPLogEnabled {
			//nolint:errcheck // output stream
			handlerCtx.GetOutErrFile().Write([]byte(
				fmt.Sprintln(fmt.Sprintf("http response error: %s", err.Error()))), //nolint:gosimple,lll // TODO: sweep through this sort of nonsense
			)
		}
		return nil, err
	}
	return r, err
}
