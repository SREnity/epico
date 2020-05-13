package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"

	//    "os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	generic_structs "github.com/SREnity/epico/structs"

	xj "github.com/basgys/goxml2json"

	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/jwt"
)

type oneloginRequest struct {
	GrantType string `json:"grant_type"`
}

type oneloginData struct {
	AccessToken string `json:"access_token"`
}

type oneloginTokens struct {
	Data []oneloginData `json:"data"`
}

func LogFatal(function string, text string, err error) {
	log.Fatalf("(Epico:%v) %v: %v\n", function, text, err)
}

func LogWarn(function string, text string, err error) {
	log.Printf("(Epico:%v) %v: %v\n", function, text, err)
}

// This function simply takes an XML response and converts it to JSON (the
//    preferred internal form of Epico).
// Args:
// apiResponse = A []byte representation of the XML API response.
func XmlResponseProcess(apiResponse []byte) []byte {

	jsonBody, err := xj.Convert(bytes.NewReader(apiResponse))
	if err != nil {
		LogFatal("XmlResponseProcess", "Error parsing XML response", err)
		return nil
	}

	return jsonBody.Bytes()

}

// Used to expand out the shorthand YAMLs with expansion vars into a series of
//    individual, expanded YAML []byte's for consumption by Epico.
// Args:
// rawYaml  = raw YAML []byte that will be tranformed into a slice of []bytes
// varsData = vars data to be expanded
func PopulateYamlSlice(rawYaml string, varsData map[string][]string) [][]byte {
	indexes := make([]string, len(varsData))            // These are keys that need to be
	depth := 0                                          //    expanded
	varValues := make(map[string]string, len(varsData)) // Actual sub values
	index := 0
	for k, _ := range varsData {
		// Load our keys into the indexes slice and instantiate varValues maps
		indexes[index] = k
		index += 1
		varValues[k] = ""
	}

	var returnSlice [][]byte
	for i, _ := range varsData[indexes[depth]] {
		// Don't need to append here because returnSlice is being passed and
		//    builds upon the old slice.
		returnSlice = populateSliceRecursion(rawYaml, varsData, indexes, depth, i, varValues, returnSlice)
	}

	return returnSlice
}

// This function collapses two map[string]interface{} json representations into
//    a single one. WARNING (TODO): This does not handle key collisions
//    gracefully.
func CollapseJson(returnsList map[string]interface{}, errorsList map[string]interface{}) []byte {
	finalList := make(map[string]interface{})

	for k, v := range returnsList {
		finalList[k] = v
	}
	for k, v := range errorsList {
		finalList[k] = v
	}

	finalJson, err := json.Marshal(finalList)
	if err != nil {
		LogFatal("CollapseJson", "Unable to Marshal final list JSON", err)
		return nil
	}

	return []byte(finalJson)
}

// Recursively searches through a JSON response using a given set of keys
//    indicating a valid response and/or error response.  It then updates the
//    provided structures with the new data.
// Vars:
// response             = A ComparableApiRequest representing the request which
//                        gave this response.
// processedJson        = The JSON response from the API request that we are
//                        parsing.
// parsedStructure      = Map we store response data in.
// parsedErrorStructure = Map we store error data in.
func ParsePostProcessedJson(response generic_structs.ComparableApiRequest, jsonKeys []map[string]string, processedJson []byte, parsedStructure map[string]interface{}, parsedErrorStructure map[string]interface{}) (map[string]interface{}, map[string]interface{}) {
	// This chunk transforms the JSON based on the YAML requirements and
	//    collapses the list.
	var unparsedStructure map[string]interface{}

	err := json.Unmarshal(processedJson, &unparsedStructure)
	if err != nil {
		LogFatal("ParsePostProcessedJson", "Error unmarshaling JSON", err)
	}

	endpointKeyValues := make(map[string]interface{})
	if len(response.EndpointKeyValues) > 0 {
		// Ignore the error here, since the JSON is valid (comes from json.Marshal)
		json.Unmarshal([]byte(response.EndpointKeyValues), &endpointKeyValues)
	}
	// Find our additional key data in the list of keys so we can work with it.
	for _, keys := range jsonKeys {
		if keys["api_call_uuid"] != response.Uuid {
			continue
		}
		// Here we handle passing in list form so we can pull multiple pieces of
		//    data from each API call.
		length, err := strconv.Atoi(keys["key_count"])
		if err != nil {
			LogFatal("ParsePostProcessedJson", "Invalid key count", err)
		}
		for i := 0; i < length; i++ {
			currentBaseKeySet := strings.Split(
				keys["current_base_key_"+strconv.Itoa(i)], ".")
			desiredBaseKeySet := strings.Split(
				keys["desired_base_key_"+strconv.Itoa(i)], ".")
			currentErrorKeySet := strings.Split(
				keys["current_error_key_"+strconv.Itoa(i)], ".")
			desiredErrorKeySet := strings.Split(
				keys["desired_error_key_"+strconv.Itoa(i)], ".")
			if len(currentBaseKeySet) == 0 || len(desiredBaseKeySet) == 0 {
				LogFatal("ParsePostProcessedJson", "Invaid current_base_key or desired_base_key.", nil)
			}

			// Run through non-error keys.
			parsedSubStructure := ParseJsonSubStructure(currentBaseKeySet, 0,
				unparsedStructure)

			// This code inserts values from parent endpoint response into sub-endpoint responses.
			// Use case example: we fetch project statistics per project from Gitlab. Project stats
			// response does not contain project name, therefore we want to include it in order for it
			// to be used in cruncher for reporting projects with stats that don't pass tests.
			if len(endpointKeyValues) > 0 {
				for i, element := range parsedSubStructure {
					unboxedElement, ok := element.(map[string]interface{})
					if !ok {
						continue
					}
					for k, v := range endpointKeyValues {
						unboxedElement[k] = v
					}
					parsedSubStructure[i] = unboxedElement
				}
			}
			// Was getting some weird byRef issues when setting the map directly
			//    equal and passing it as a param.
			newVar := addJsonKeyStructure(desiredBaseKeySet, 0, parsedStructure,
				parsedSubStructure, true)
			parsedStructure = newVar.(map[string]interface{})

			// Run through error keys.
			// These aren't added explicitly to the key set (aren't always going
			//    to be there), so we need to check for nils.
			if _, ok := unparsedStructure[currentErrorKeySet[0]]; ok {
				parsedSubStructure = ParseJsonSubStructure(currentErrorKeySet, 0,
					unparsedStructure)
				// Was getting some weird byRef issues when setting the map
				//    directly equal and passing it as a param.
				newVar = addJsonKeyStructure(desiredErrorKeySet, 0, parsedErrorStructure,
					parsedSubStructure, false)
				parsedErrorStructure = newVar.(map[string]interface{})
			}
		}
	}
	return parsedStructure, parsedErrorStructure
}

// Loops through a JSON response (usually one converted from XML) and removes
//    the unnecessary/repeating tags often used by XML structures.
// Vars:
// tag      = Unwanted tag to be removed from the structure.
// jsonBody = JSON that we want to remove the tag from.
func RemoveXmlTagFromJson(tag string, jsonBody []byte) []byte {

	bracketCount := 0
	itemCount := make([]int, 0)
	cursor := 0
	processedJson := bytes.Buffer{}

	for i, v := range jsonBody {
		// Track quotes too and don't count {} inside quotes.

		sliceIndex := intInSlice(bracketCount, itemCount)
		if sliceIndex > -1 {
			processedJson.WriteString(string(jsonBody[cursor:(i - 1)]))
			itemCount = append(itemCount[:sliceIndex],
				itemCount[(sliceIndex+1):]...)
			cursor = i
		} else if i == len(jsonBody)-1 {
			processedJson.WriteString(string(jsonBody[cursor:(i + 1)]))
			break
		}

		if string(v) == "}" {
			bracketCount = bracketCount - 1
			// TODO: What happens when an incomplete response is returned.
		} else if i < len(jsonBody)-(len(tag)+6) {
			if string(jsonBody[i:i+(len(tag)+5)]) == "{\""+tag+"\": " {
				processedJson.WriteString(string(
					jsonBody[cursor:i]))
				cursor = i + (len(tag) + 5)
				itemCount = append(itemCount, bracketCount)
				bracketCount = bracketCount + 1
			} else if string(v) == "{" {
				bracketCount = bracketCount + 1
			}
		}
	}
	return processedJson.Bytes()
}

// Peeks at a standard XML response for paging indicators.
// Vars:
// response     = The XML response in []byte form.
// responseKeys = The split list of keys to find the paging value.
// oldPageValue = The previous page value.
// peekParams   = Unused, plugin-specific params.
func DefaultXmlPagingPeek(response []byte, responseKeys []string, oldPageValue interface{}, peekParams []string) (interface{}, bool) {

	jsonResponse := XmlResponseProcess(response)

	return DefaultJsonPagingPeek(jsonResponse, responseKeys, oldPageValue,
		peekParams)

}

// Peeks at a standard JSON response for paging indicators.
// Vars:
// response     = The JSON response in []byte form.
// responseKeys = The split list of keys to find the paging value.
// oldPageValue = The previous page value.
// peekParams   = Unused, plugin-specific params.
func DefaultJsonPagingPeek(response []byte, responseKeys []string, oldPageValue interface{}, peekParams []string) (interface{}, bool) {

	if len(response) < 4 || response == nil {
		return interface{}(nil), false
	}
	var responseMap map[string]interface{}
	err := json.Unmarshal(response, &responseMap)
	if err != nil {
		var responseSlice []interface{}
		err1 := json.Unmarshal(response, &responseSlice)
		if err1 != nil {
			LogFatal("DefaultJsonPagingPeek", "Unable to Unmarshal peek JSON ("+string(response)+")",
				err1)
		} else {
			LogWarn("DefaultJsonPagingPeek", "Slice JSON response - no paging.",
				err)
			return interface{}(nil), false
		}
	}
	// New page value is nil.
	var pageValue interface{}
	// Loop through the key list and set pageValue to each successive key to
	//   drill down.  We should never hit a list or a string (should always be
	//   a map) until we reach this value since there should always only be one
	//   per API response.
	for _, v := range responseKeys {
		if pageValue == nil {
			pageValue = responseMap[v]
		} else {
			pageValue = pageValue.(map[string]interface{})[v]
		}
	}

	if pageValue == oldPageValue {
		pageValue = nil
	}
	return pageValue, (pageValue != "" && pageValue != nil)

}

// Peeks at a standard JSON response for paging indicators.
// Vars:
// response     = The JSON response in []byte form.
// responseKeys = The split list of keys to find the paging value.
// oldPageValue = The previous page value.
// peekParams   = Params specific to this function - expecting:
//                [0] => Valid regex with paging param located in the first
//                    subexpression group in ()s ex: <([^>]*)>; rel=\"next\"
func RegexJsonPagingPeek(response []byte, responseKeys []string, oldPageValue interface{}, peekParams []string) (interface{}, bool) {

	re, err := regexp.Compile(peekParams[0])
	if err != nil {
		LogFatal("RegexJsonPagingPeek", "Invalid regex provided in YAML", err)
	}
	pagingValue, valueIsPresent := DefaultJsonPagingPeek(response, responseKeys,
		oldPageValue, peekParams)
	if !valueIsPresent {
		return interface{}(nil), false
	}

	switch reflect.TypeOf(pagingValue).String() {
	case "[]interface {}":
		for _, v := range pagingValue.([]interface{}) {
			// TODO: More robust handling here if we don't have a string.
			submatches := re.FindStringSubmatch(v.(string))
			if len(submatches) > 1 {
				return interface{}(submatches[1]), true
			}
		}
	default: // pagingValue is likely string or nil
		// TODO: More robust handling here if we don't have a string.
		submatches := re.FindStringSubmatch(pagingValue.(string))
		if len(submatches) > 1 {
			return interface{}(submatches[1]), true
		}
	}

	return interface{}(nil), false
}

// Peeks at a standard JSON response for paging indicators that need to be
//    calculated.
// Vars:
// response     = The JSON response in []byte form.
// responseKeys = The split list of keys to find the paging value.
//                [0]    => Length of current page value key and length of per
//                              page value key in csv
//                [1..X] => Key parts for current page value key
//                [X..Y] => Key parts for per page value key
//                [Y..Z] => Key parts for total results value key
// oldPageValue = The previous page value.
// peekParams   = Unused, plugin-specific params.
func CalculatePagingPeek(response []byte, responseKeys []string, oldPageValue interface{}, peekParams []string) (interface{}, bool) {

	if len(responseKeys) < 4 {
		LogFatal("CalculatePagingPeek",
			"Unable to calculate paging without at least three keys", nil)
	}
	splitLengthKeys := strings.Split(responseKeys[0], ",")

	if len(splitLengthKeys) != 2 {
		LogFatal("CalculatePagingPeek",
			"Invalid length keys for paging calculation", nil)
	}
	pageKeySplit, err := strconv.Atoi(splitLengthKeys[0])
	if err != nil {
		LogFatal("CalculatePagingPeek",
			"Non integer length keys for paging calculation", nil)
	}

	perPageKeySplit, err := strconv.Atoi(splitLengthKeys[1])
	if err != nil {
		LogFatal("CalculatePagingPeek",
			"Non integer length keys for paging calculation", nil)
	}

	// Remove our calculated length values after using them.
	responseKeys = responseKeys[1:]

	var responseMap map[string]interface{}
	err = json.Unmarshal(response, &responseMap)
	if err != nil {
		var responseSlice []interface{}
		err1 := json.Unmarshal(response, &responseSlice)
		if err1 != nil {
			LogFatal("DefaultJsonPagingPeek", "Unable to Unmarshal peek JSON",
				err)
		} else {
			LogWarn("DefaultJsonPagingPeek", "Slice JSON response - no paging.",
				err)
			return interface{}(nil), false
		}
	}
	// New page value is nil.
	// Ensure we got the key
	//if _, ok := responseMap[responseKeys[0]]; ok {}

	var pageValue, perPageValue, totalCountValue interface{}
	// Loop through the key list and set pageValue to each successive key to
	//   drill down.  We should never hit a list or a string (should always be
	//   a map) until we reach this value since there should always only be one
	//   per API response.

	// Loop through the keys to find the total value.
	for _, v := range responseKeys[pageKeySplit+perPageKeySplit:] {
		if totalCountValue == nil {
			totalCountValue = responseMap[v]
		} else {
			totalCountValue = totalCountValue.(map[string]interface{})[v]
		}
	}

	// Loop through the keys to find the per page value.
	for _, v := range responseKeys[pageKeySplit : pageKeySplit+perPageKeySplit] {
		if perPageValue == nil {
			perPageValue = responseMap[v]
		} else {
			perPageValue = perPageValue.(map[string]interface{})[v]
		}
	}

	// If the per page value is >= total count, no more pages.
	if totalCountValue == nil || perPageValue == nil ||
		totalCountValue.(float64) <= perPageValue.(float64) {
		return interface{}(nil), false
	}

	// Loop through the keys to find the current page value.
	for _, v := range responseKeys[:pageKeySplit] {
		if pageValue == nil {
			pageValue = responseMap[v]
		} else {
			pageValue = pageValue.(map[string]interface{})[v]
		}
	}

	if pageValue != nil && perPageValue != nil && totalCountValue != nil {
		if pageValue.(float64)*perPageValue.(float64) < totalCountValue.(float64) {
			pageValue = pageValue.(float64) + 1
		} else { // If we're over total count or equal, then it's done.
			return interface{}(nil), false
		}
	}

	return pageValue, (pageValue != "" && pageValue != nil)

}

// Takes a map of requests to their []byte responses, iterates through them to
//    pull the desired data (and errors), and compiles the final result.
// Vars:
// apiResponseMap = A map of API requests made and their corresponding responses
func DefaultJsonPostProcess(apiResponseMap map[generic_structs.ComparableApiRequest][]byte, jsonKeys []map[string]string) []byte {

	parsedStructure := make(map[string]interface{})
	parsedErrorStructure := make(map[string]interface{})

	for request, response := range apiResponseMap {

		// Catch JSON slices that don't have a map at the root
		var jsonSlice []interface{}
		err := json.Unmarshal(response, &jsonSlice)
		if err == nil {
			// We're here, because the response is an array of items
			LogWarn("DefaultJsonPostProcess", "JSON is a slice - building map.",
				err)
			// Maybe more efficient, but less robust than build and marshal?
			response = append([]byte("{\"items\":"),
				append(response, []byte("}")...)...)
			// Add our new key we created to the base key expected.
			// Only prepend it if the response is an array, otherwise we're going to
			// mess up the expected base key from a map response
			for i, v := range jsonKeys {
				if v["api_call_uuid"] == request.Uuid {
					length, err := strconv.Atoi(v["key_count"])
					if err != nil {
						LogFatal("DefaultJsonPostProcess",
							"Non-integer key_count is invalid", err)
					}
					for ci := 0; ci < length; ci++ {
						keyString := "current_base_key_" + strconv.Itoa(ci)
						if val, ok := jsonKeys[i][keyString]; ok && len(val) == 0 {
							jsonKeys[i][keyString] = "items"
						} else {
							jsonKeys[i][keyString] = "items." + val
						}
					}
					// Duplicated names aren't allowed, but do happen with sub-
					//    endpoints.  In which case, all other input fields
					//    like the current base key should be the same.
					break
				}
			}
		}

		structureVar, errorVar := ParsePostProcessedJson(request, jsonKeys,
			response, parsedStructure, parsedErrorStructure)
		parsedStructure = structureVar
		parsedErrorStructure = errorVar
	}

	returnJson := CollapseJson(parsedStructure, parsedErrorStructure)
	return returnJson

}

// Auth function for basic username/password auth implementations.  Takes a
//    username and password and constructs the Authorization header.
// Vars:
// apiRequest = The ApiRequest to be used.
// authParams = JWT params in the order of:
//              [0] => username
//              [1] => password
func BasicAuth(apiRequest generic_structs.ApiRequest, authParams []string) generic_structs.ApiRequest {

	apiRequest.FullRequest.SetBasicAuth(authParams[0], authParams[1])

	return apiRequest
}

// Removed in favor of simplicity - just use CustomHeaderAuth
//func TokenAuth( apiRequest generic_structs.ApiRequest, authParams []string ) generic_structs.ApiRequest

// Auth function for custom querystring auth implementations.  Takes an
//    alternating list of keys/values and constructs the querystring.
// Vars:
// apiRequest = The ApiRequest to be used.
// authParams = Auth params in any quantity, alternating key then value:
//              [x] => header key
//              [x+1] => header value
func CustomQuerystringAuth(apiRequest generic_structs.ApiRequest, authParams []string) generic_structs.ApiRequest {

	if len(authParams)%2 != 0 {
		LogFatal("CustomQuerystringAuth",
			"Invalid querystring params - must have a value for every key.",
			nil)
	}

	q := apiRequest.FullRequest.URL.Query()
	for i := 0; i < len(authParams)-1; i += 2 {
		// Don't duplicate keys that are the same.
		found := false
		for _, v := range q[authParams[i]] {
			if v == authParams[i+1] {
				found = true
			}
		}
		if !found {
			q.Add(authParams[i], authParams[i+1])
		}
	}
	apiRequest.FullRequest.URL.RawQuery = q.Encode()

	return apiRequest
}

// Auth function for custom header auth implementations.  Takes an alternating
//    list of keys/values and constructs the header.
// Vars:
// apiRequest = The ApiRequest to be used.
// authParams = Auth params in any quantity, alternating key then value:
//              [x] => header key
//              [x+1] => header value
func CustomHeaderAuth(apiRequest generic_structs.ApiRequest, authParams []string) generic_structs.ApiRequest {

	if len(authParams)%2 != 0 {
		LogFatal("CustomHeaderAuth",
			"Invalid header params - must have a value for every key.", nil)
	}
	for i := 0; i < len(authParams)-1; i += 2 {
		// Don't duplicate keys that are the same.
		found := false
		for _, v := range apiRequest.FullRequest.Header[authParams[i]] {
			if v == authParams[i+1] {
				found = true
			}
		}
		if !found {
			apiRequest.FullRequest.Header.Add(authParams[i], authParams[i+1])
		}
	}

	return apiRequest
}

// Auth function for custom header auth implementations that also require basic
//    auth.  Takes the basic auth keys username/password and an alternating
//    list of keys/values and constructs the header.
// Vars:
// apiRequest = The ApiRequest to be used.
// authParams = Auth params in any quantity, alternating key then value:
//              [0] => username
//              [1] => password
//              [x] => header key
//              [x+1] => header value
func CustomHeaderAndBasicAuth(apiRequest generic_structs.ApiRequest, authParams []string) generic_structs.ApiRequest {

	apiRequest = BasicAuth(apiRequest, authParams[:2])
	apiRequest = CustomHeaderAuth(apiRequest, authParams[2:])

	return apiRequest
}

// OneloginAuth performs some additional magic that is specific to OneLogin
func OneloginAuth(apiRequest generic_structs.ApiRequest, authParams []string) generic_structs.ApiRequest {
	reqDataStruct := &oneloginRequest{"client_credentials"}
	reqDataBytes, err := json.Marshal(reqDataStruct)
	if err != nil {
		LogFatal("OneloginAuth", "Failed to marshal auth request data", err)
	}

	req, err := http.NewRequest("POST", authParams[2], bytes.NewBuffer(reqDataBytes))
	if err != nil {
		LogFatal("OneloginAuth", "Failed to initialize HTTP request for auth", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("client_id:%s, client_secret:%s", authParams[0], authParams[1]))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		LogFatal("OneloginAuth", "Failed to perform request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		LogFatal("OneloginAuth", fmt.Sprintf("Expected 200, got %d", resp.StatusCode), nil)
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		LogFatal("OneloginAuth", "Failed to read token data response body", err)
	}

	var tokenData oneloginTokens
	if err := json.Unmarshal(bodyBytes, &tokenData); err != nil {
		LogFatal("OneloginAuth", "Failed to unmarshal token data JSON", err)
	}

	return CustomHeaderAuth(apiRequest, []string{"Authorization", fmt.Sprintf("bearer %s", tokenData.Data[0].AccessToken)})
}

// Auth function for session auth implementations.  Takes provided params and
//    retrieves the session token from the designated key then updates the
//    header of the ApiRequest.
// Vars:
// apiRequest = The ApiRequest to be used.
// authParams = Session params in the order of:
//              [0] => Where the token is in the response e.g. "data.token"
//              [1] => Custom header key e.g. "Authorization" or "Auth-Token"
//              [2] => Custom header pre-token value e.g. "token "
//              [3] => Session token URL
//              [x] => Session key
//              [x+1] => Session value
func SessionTokenAuth(apiRequest generic_structs.ApiRequest, authParams []string) generic_structs.ApiRequest {

	if len(authParams[4:])%2 != 0 {
		LogFatal("SessionTokenAuth",
			"Invalid header params - must have a value for every key.", nil)
	}

	bodyMap := map[string]string{}

	if len(authParams) > 4 {
		for i := 4; i < len(authParams)-1; i += 2 {
			bodyMap[authParams[i]] = authParams[i+1]
		}
	}

	jsonString, err := json.Marshal(bodyMap)
	if err != nil {
		LogFatal("SessionTokenAuth", "Error marshaling JSON", err)
	}

	// TODO: Break this out to allow URL encoded session function as well
	resp, err := http.Post(authParams[3], "application/json", bytes.NewBuffer(jsonString))
	if err != nil {
		LogFatal("SessionTokenAuth", "Error running the session POST request",
			err)
	}
	defer resp.Body.Close()

	// TODO: Use a better technique instead of raising an error
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		LogFatal("SessionTokenAuth", fmt.Sprintf("Expected response status 2xx, got %d", resp.StatusCode), nil)
	}

	// TODO: Handle failed connections better / handle retry? Golang "Context"?
	// i/o timeoutpanic: runtime error: invalid memory address or nil pointer dereference
	// [signal SIGSEGV: segmentation violation code=0x1 addr=0x40 pc=0x6aa2ba]

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		LogFatal("SessionTokenAuth", "Error reading request body", err)
	}

	var jsonResponseMap interface{}
	err = json.Unmarshal(body, &jsonResponseMap)
	if err != nil {
		LogFatal("SessionTokenAuth", "Unable to unmarshal session JSON",
			err)
	}

	var tokenValue string
	for _, v := range strings.Split(authParams[0], ".") {
		if reflect.TypeOf(jsonResponseMap.(map[string]interface{})[v]).String() == "string" {
			tokenValue = jsonResponseMap.(map[string]interface{})[v].(string)
		} else {
			jsonResponseMap = jsonResponseMap.(map[string]interface{})[v]
		}
	}

	customParams := authParams[1:3]
	customParams[1] = customParams[1] + tokenValue

	return CustomHeaderAuth(apiRequest, customParams)
}

// Auth function for Oauth 2 2-legged implementations.  Takes Oauth params and
//    preps the http client attached to the ApiRequest.
// Vars:
// apiRequest = The ApiRequest to be used.
// authParams = Oauth2 params in the order of:
//              [0] => client ID
//              [1] => client secret
//              [2] => scopes (csv)
//              [3] => token url
//              [4] => endpoint params (string with ":" key/value separator and
//                     "," between entries => e.g. x:y,x:z,a:b)
func Oauth2TwoLegAuth(apiRequest generic_structs.ApiRequest, authParams []string) generic_structs.ApiRequest {
	values := url.Values{}
	for _, v := range strings.Split(authParams[4], ",") {
		colonSplit := strings.Index(v, ":")
		if colonSplit < 0 {
			LogFatal("Oauth2TwoLegAuth", "Invalid endpoint params", nil)
		}
		values.Add(v[:colonSplit], v[colonSplit+1:])
	}
	cfg := &clientcredentials.Config{
		ClientID:       authParams[0],
		ClientSecret:   authParams[1],
		Scopes:         strings.Split(authParams[2], ","),
		TokenURL:       authParams[3],
		EndpointParams: values,
	}

	ctx := context.Background()
	apiRequest.Client = cfg.Client(ctx)

	return apiRequest
}

// Auth function for JWT implementations.  Takes JWT params and preps the http
//    client attached to the ApiRequest.
// Vars:
// apiRequest = The ApiRequest to be used.
// authParams = JWT params in the order of:
//              [0] => email
//              [1] => private key
//              [2] => private key id
//              [3] => scopes (comma delimited string)
//              [4] => token url
func JwtAuth(apiRequest generic_structs.ApiRequest, authParams []string) generic_structs.ApiRequest {
	cfg := &jwt.Config{
		Email:        authParams[0],
		PrivateKey:   []byte(authParams[1]),
		PrivateKeyID: authParams[2],
		Scopes:       strings.Split(authParams[3], ","),
		TokenURL:     authParams[4],
	}
	// TODO: No blanks.
	//if cfg.TokenURL == "" {
	//}

	ctx := context.Background()
	apiRequest.Client = cfg.Client(ctx)

	return apiRequest
}

// Used at build-time for plugins to actually expand the variables in our new
//    YAML files.
// Vars:
// rawYaml   = Raw YAML we are expanding.
// varsData  = The vars data we are expanding.
// indexes   = List of variables being expanded.
// depth     = Count of recursion depth.
// listIndex = Count of variable being expanded from the []string of vars.
// varValues = Values being replaced.
func populateSliceRecursion(rawYaml string, varsData map[string][]string, indexes []string, depth int, listIndex int, varValues map[string]string, returnSlice [][]byte) [][]byte {
	if depth == len(indexes)-1 { // If we're at the end of the keys list.
		varValues[indexes[depth]] = varsData[indexes[depth]][listIndex]
		newYaml := rawYaml
		for k, v := range varValues {
			newYaml = strings.Replace(newYaml, "{{"+k+"}}", v, -1)
			returnSlice = append(returnSlice, []byte(newYaml))
		}
	} else { // If we have more keys to go.
		varValues[indexes[depth]] = varsData[indexes[depth]][listIndex]
		for i, _ := range varsData[indexes[depth+1]] {
			returnSlice = append(returnSlice, populateSliceRecursion(rawYaml, varsData, indexes, depth+1, i, varValues, returnSlice)...)
		}
	}

	return returnSlice
}

// Recursively drill down into JSON to find the value of a specific key set
//    (e.g. {"X": { "Y": { "Z": [ 1, 2, 3 ] } } } with key set "X.Y.Z" would
//    return [ 1, 2, 3 ]).
// Vars:
// kSet         = Key set being searched for.
// count        = Recursive depth count.
// subStructure = Structure being plumbed.
func ParseJsonSubStructure(kSet []string, count int, subStructure interface{}) []interface{} {
	// Start by marshaling our interface{} into a map which everything in JSON
	//    should be if there are more subkeys.
	var subStructureMap map[string]interface{}
	// If it isn't a map, it's a list of maps, so we'll create one here to use
	//    if necessary.
	subStructureListMap := []map[string]interface{}{}

	marshaledInterface, err := json.Marshal(subStructure)
	if err != nil {
		LogFatal("ParseJsonSubStructure", "Error marshaling JSON", err)
		return nil
	}

	err = json.Unmarshal(marshaledInterface, &subStructureMap)
	if err != nil {
		if string(marshaledInterface) != "\"\"" {
			err = json.Unmarshal(marshaledInterface, &subStructureListMap)
			if err != nil {
				// TODO: Failure shouldn't wipe the map.  Really should
				//    unmarshal elsewhere and check.
				LogFatal("ParseJsonSubStructure", "Error unmarshaling JSON", err)
			}
		}
	} else { // Wasn't a list? Append it to the blank to make a list.
		subStructureListMap = append(subStructureListMap, subStructureMap)
	}

	finalInterfaceList := []interface{}{}
	for _, v := range subStructureListMap {
		if count == len(kSet)-1 {
			// We're on the last piece of the key here.
			blankInterfaceList := []interface{}{}
			if reflect.TypeOf(v[kSet[count]]) ==
				reflect.TypeOf(blankInterfaceList) {
				// If we do have a list, then return it.
				finalInterfaceList = append(finalInterfaceList,
					v[kSet[count]].([]interface{})...)
			} else if !reflect.DeepEqual(v[kSet[count]], "") &&
				v[kSet[count]] != nil {
				// If we aren't a nil string (or nil), then we have a map that
				//    should be transformed into a list.
				finalInterfaceList = append(finalInterfaceList,
					v[kSet[count]])
			}
		} else {
			// We don't want [ <nil> ], so just don't append if it's nil.
			if v[kSet[count]] != nil {
				finalInterfaceList = append(
					finalInterfaceList, ParseJsonSubStructure(
						kSet, count+1, v[kSet[count]])...)
			}
		}
	}
	return finalInterfaceList
}

// Adds a data item at the specified key set within the JSON structure.  (e.g.
//    adding [ 4 ] to {"X": { "Y": { "Z": [ 1, 2, 3 ] } } } with key set "X.Y.Z"
//    would return {"X": { "Y": { "Z": [ 1, 2, 3, 4 ] } } })
// Vars:
// kSet             = Key set being searched for.
// count            = Recursive depth count.
// currentStructure = Structure being added to.
// newStructure     = Structure being added.
// force            = Forces the addition of an empty structure if
//                    currentStructure is empty or nil.
func addJsonKeyStructure(kSet []string, count int, currentStructure map[string]interface{}, newStructure []interface{}, force bool) interface{} {
	if !force && len(newStructure) == 0 {
		return marshalToInterface(currentStructure)
	}

	if count == len(kSet)-1 {
		if _, ok := currentStructure[kSet[count]]; !ok {
			currentStructure[kSet[count]] = marshalToInterface(newStructure)
		} else { // if key does exist in current substructure
			if len(newStructure) > 0 {
				currentStructure[kSet[count]] = marshalToInterface(append(
					currentStructure[kSet[count]].([]interface{}), newStructure...))
			}
		}
		return marshalToInterface(currentStructure)
	} else {
		if _, ok := currentStructure[kSet[count]]; !ok {
			currentStructure[kSet[count]] = marshalToInterface(
				addJsonKeyStructure(
					kSet, count+1, make(map[string]interface{}), newStructure, force))
		} else {
			if len(newStructure) > 0 {
				currentStructure[kSet[count]] = marshalToInterface(
					addJsonKeyStructure(
						kSet, count+1, currentStructure[kSet[count]].(map[string]interface{}), newStructure, force))
				// TODO: Catch panics here if they try to put errors in with
				//    different key depths.
			}
		}
		return marshalToInterface(currentStructure)
	}
}

// A simple Marshal/Unmarshal to force the structure into a JSON-friendly
//    format.
func marshalToInterface(data interface{}) interface{} {
	jsonIntermediary, err := json.Marshal(data)
	if err != nil {
		LogFatal("marshalToInterface", "Unable to Marshal interface to JSON", err)
	}
	var typeParsedStructure interface{}
	json.Unmarshal([]byte(jsonIntermediary), &typeParsedStructure)
	return typeParsedStructure
}

// Finds where a specific int exists in an []int.
func intInSlice(a int, list []int) int {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return -1
}
