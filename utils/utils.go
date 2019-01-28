package utils

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "log"
//    "os"
    "reflect"
    "strings"

    generic_structs "github.com/SREnity/epico/structs"

    xj "github.com/basgys/goxml2json"
    "github.com/satori/go.uuid"

    "golang.org/x/oauth2/jwt"
)


func LogFatal( function string, text string, err error ) {
    log.Fatalf("(Epico:%v) %v: %v\n", function, text, err)
}


func LogWarn( function string, text string, err error ) {
    log.Printf("(Epico:%v) %v: %v\n", function, text, err)
}


// This function simply takes an XML response and converts it to JSON (the
//    preferred internal form of Epico).
// Args:
// apiResponse = A []byte representation of the XML API response.
func XmlResponseProcess( apiResponse []byte ) []byte {

    jsonBody, err := xj.Convert( bytes.NewReader( apiResponse ) )
    if err != nil {
        LogFatal("XmlResponseProcess", "Error parsing XML response", err)
        return nil
    }

    return jsonBody.Bytes()

}


// This function is used during build-time of a plugin to expand out the
//    shorthand YAMLs with expansion vars into a series of individual, expanded
//    YAML files for consumption by Epico.
// Args:
// configDir =
// rawYaml  =
// apiType  =
// varsData     =
// TODO: Fix assumptions and variables here. configDir => yamlDir, no apiType, should overwrite files in dir... 
func PopulateYamlCache( configDir string, rawYaml string, apiType string, varsData map[string][]string ) {

    // TODO: This should delete everything in the dir instead of read.
//    files, err := ioutil.ReadDir(configDir)
//    if err != nil {
//        fmt.Printf("Unable to read cache directory: %v\nCreating...\n", err)
//        if _, err1 := os.Stat(configDir); os.IsNotExist(err1) {
//            err1 = os.MkdirAll(configDir, 0755)
//            if err1 != nil {
//                fmt.Printf("Unable to create cache directory: %v\n", err)
//            }
//        }
//    }

    indexes := make([]string, len(varsData))
    depth := 0
    varValues := make(map[string]string, len(varsData))
    index := 0
    for k, _ := range varsData {
        indexes[index] = k
        index += 1
        varValues[k] = ""
    }

    for i, _ := range varsData[indexes[depth]] {
        populateCacheRecursion( configDir, rawYaml, varsData, indexes, depth, i, varValues )
    }
}


// This function collapses two map[string]interface{} json representations into
//    a single one. WARNING (TODO): This does not handle key collisions
//    gracefully.
func CollapseJson( returnsList map[string]interface{}, errorsList map[string]interface{} ) []byte {
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
func ParsePostProcessedJson( response generic_structs.ComparableApiRequest, processedJson []byte, parsedStructure map[string]interface{}, parsedErrorStructure map[string]interface{} ) {
    // This chunk transforms the JSON based on the YAML requirements and
    //    collapses the list.
    var unparsedStructure map[string]interface{}

    err := json.Unmarshal(processedJson, &unparsedStructure)
    if err != nil {
        fmt.Printf("ParsePostProcessedJson", "Error unmarshaling JSON", err)
    }


    cbkSet := strings.Split( response.CurrentBaseKey, "." )
    dbkSet := strings.Split( response.DesiredBaseKey, "." )
    cekSet := strings.Split( response.CurrentErrorKey, "." )
    dekSet := strings.Split( response.DesiredErrorKey, "." )
    if len(cbkSet) < 1 || len(dbkSet) < 1 {
        LogFatal("ParsePostProcessedJson", "Invaid current_base_key or desired_base_key.", nil)
    }

    // Run through non-error keys.
    parsedSubStructure := parseJsonSubStructure(
        cbkSet, 0, unparsedStructure )
    // Was getting some weird byRef issues when setting the map directly
    //    equal and passing it as a param.
    newVar := addJsonKeyStructure(
        dbkSet, 0, parsedStructure, parsedSubStructure, true)
    parsedStructure = newVar.(map[string]interface{})


    // Run through error keys.
    // These aren't added explicitly to the key set, so we need to check for
    //    nils.
    if _, ok := unparsedStructure[cekSet[0]]; ok {
        parsedSubStructure = parseJsonSubStructure(
            cekSet, 0, unparsedStructure )
        // Was getting some weird byRef issues when setting the map directly
        //    equal and passing it as a param.
        newVar = addJsonKeyStructure(
            dekSet, 0, parsedErrorStructure, parsedSubStructure, false)
        parsedErrorStructure = newVar.(map[string]interface{})
    }

}


// Loops through a JSON response (usually one converted from XML) and removes
//    the unnecessary/repeating tags often used by XML structures.
// Vars:
// tag      = Unwanted tag to be removed from the structure.
// jsonBody = JSON that we want to remove the tag from.
func RemoveXmlTagFromJson( tag string, jsonBody []byte ) []byte {

    bracketCount := 0
    itemCount := make([]int, 0)
    cursor := 0
    processedJson := bytes.Buffer{}

    for i, v := range jsonBody {
        // Track quotes too and don't count {} inside quotes.

        sliceIndex := intInSlice( bracketCount, itemCount )
        if sliceIndex > -1 {
            processedJson.WriteString( string( jsonBody[cursor:(i-1)] ) )
            itemCount = append( itemCount[:sliceIndex],
                itemCount[(sliceIndex+1):]... )
            cursor = i
        } else if i == len(jsonBody) - 1 {
            processedJson.WriteString(string(jsonBody[cursor:(i+1)]))
            break
        }

        if string(v) == "}" {
            bracketCount = bracketCount - 1
        // TODO: What happens when an incomplete response is returned.
        } else if i < len(jsonBody) - (len(tag)+6) {
            if string(jsonBody[i:i+(len(tag)+5)]) == "{\"" + tag + "\": " {
                processedJson.WriteString( string(
                    jsonBody[cursor:i] ) )
                cursor = i + (len(tag)+5)
                itemCount = append( itemCount, bracketCount )
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
func DefaultXmlPagingPeek( response []byte, responseKeys []string, oldPageValue interface{} ) ( interface{}, bool ) {

    jsonResponse := utils.XmlResponseProcess( response )

    return utils.DefaultJsonPagingPeek( jsonResponse, responseKeys, oldPageValue )

}


// Peeks at a standard JSON response for paging indicators.
// Vars:
// response     = The JSON response in []byte form.
// responseKeys = The split list of keys to find the paging value.
// oldPageValue = The previous page value.
func DefaultJsonPagingPeek( response []byte, responseKeys []string, oldPageValue interface{} ) ( interface{}, bool ) {

    var responseMap map[string]interface{}
    err := json.Unmarshal(response, &responseMap)
    if err != nil {
        LogFatal("DefaultJsonPagingPeek", "Unable to Unmarshal peek JSON", err)
        return interface{}(nil), false
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
    return pageValue, ( pageValue != "" && pageValue != nil )

}


// Takes a map of requests to their []byte responses, iterates through them to 
//    pull the desired data (and errors), and compiles the final result.
// Vars:
// apiResponseMap = A map of API requests made and their corresponding responses
func DefaultJsonPostProcess( apiResponseMap map[generic_structs.ComparableApiRequest][]byte ) []byte {

    parsedStructure := make(map[string]interface{})
    parsedErrorStructure := make(map[string]interface{})

    for response, apiResponse := range apiResponseMap {
        ParsePostProcessedJson( response, apiResponse, parsedStructure,
            parsedErrorStructure )
    }

    returnJson := CollapseJson( parsedStructure, parsedErrorStructure )
    return returnJson

}

func JwtAuth( apiRequest generic_structs.ApiRequest, authParams []string ) generic_structs.ApiRequest {
    cfg := &jwt.Config{
        Email: authParams[0],
        PrivateKey: []byte(authParams[1]),
        PrivateKeyID: authParams[2],
        Scopes: strings.Split( authParams[3], "," ),
        TokenURL: authParams[4],
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
// configDir = Directory to write new YAML files to.
// rawYaml   = Raw YAML we are expanding.
// varsData  = The vars we are expanding.
// indexes   = List of variables being expanded.
// depth     = Count of recursion depth.
// listIndex = Count of variable being expanded.
// varValues = Values being replaced. 
// TODO: Hunt down this logic and fix description above?
func populateCacheRecursion( configDir string, rawYaml string, varsData map[string][]string, indexes []string, depth int, listIndex int, varValues map[string]string ) {
    if depth == len(indexes) - 1 {
        varValues[indexes[depth]] = varsData[indexes[depth]][listIndex]
        createCacheFile( configDir, rawYaml, varValues )
    } else {
        varValues[indexes[depth]] = varsData[indexes[depth]][listIndex]
        for i, _ := range varsData[indexes[depth + 1]] {
            populateCacheRecursion( configDir, rawYaml, varsData, indexes, depth + 1, i, varValues )
        }
    }
}


// Used at build-time for plugins to actually create the expanded YAML files.
// Vars:
// configDir    = Directory to write new YAML files to.
// originalYaml = Raw YAML we are expanding.
// varValues    = The vars we are expanding.
func createCacheFile( configDir string, originalYaml string, varValues map[string]string ) {
    newYaml := originalYaml
    for k, v := range varValues {
        newYaml = strings.Replace( newYaml, "{{" + k + "}}", v, -1 )
    }

    newUuid, err := uuid.NewV4()
    if err != nil {
        LogFatal("createCacheFile", "Unable to generate new UUID", err)
    }

    err = ioutil.WriteFile(configDir + newUuid.String() + ".yaml", []byte(newYaml), 0755)
    if err != nil {
        LogFatal("createCacheFile", "Error writing YAML API defnition", err)
    }

}


// Recursively drill down into JSON to find the value of a specific key set
//    (e.g. {"X": { "Y": { "Z": [ 1, 2, 3 ] } } } with key set "X.Y.Z" would
//    return [ 1, 2, 3 ]).
// Vars:
// kSet         = Key set being searched for.
// count        = Recursive depth count.
// subStructure = Structure being plumbed.
func parseJsonSubStructure( kSet []string, count int, subStructure interface{} ) []interface{} {
    // Start by marshaling our interface{} into a map which everything in JSON
    //    should be if there are more subkeys.
    var subStructureMap map[string]interface{}
    // If it isn't a map, it's a list of maps, so we'll create one here to use
    //    if necessary.
    subStructureListMap := []map[string]interface{}{}

    marshaledInterface, err := json.Marshal(subStructure)
    if err != nil {
        LogFatal("parseJsonSubStructure", "Error marshaling JSON", err)
        return nil
    }

    err = json.Unmarshal(marshaledInterface, &subStructureMap)
    if err != nil {
        if string(marshaledInterface) != "\"\"" {
            err = json.Unmarshal(marshaledInterface, &subStructureListMap)
            if err != nil {
                // TODO: Failure shouldn't wipe the map.  Really should
                //    unmarshal elsewhere and check.
                LogFatal("parseJsonSubStructure", "Error unmarshaling JSON", err)
            }
        }
    } else { // Wasn't a list? Append it to the blank to make a list.
        subStructureListMap = append( subStructureListMap, subStructureMap )
    }

    finalInterfaceList := []interface{}{}
    for _, v := range subStructureListMap {
        if count == len(kSet) - 1 {
            // We're on the last piece of the key here.
            blankInterfaceList := []interface{}{}
            if reflect.TypeOf(v[kSet[count]]) ==
                  reflect.TypeOf(blankInterfaceList) {
                // If we do have a list, then return it.
                finalInterfaceList = append(
                    finalInterfaceList, v[kSet[count]].([]interface{})...)
            } else if !reflect.DeepEqual( v[kSet[count]], "" ) {
                // If we aren't a nil string, then we have a map that should
                //    be transformed into a list.
                finalInterfaceList = append(
                    finalInterfaceList, v[kSet[count]] )
            }
        } else {
            // We don't want [ <nil> ], so just don't append if it's nil.
            if v[kSet[count]] != nil {
                finalInterfaceList = append(
                    finalInterfaceList, parseJsonSubStructure(
                        kSet, count + 1, v[kSet[count]] )... )
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
func addJsonKeyStructure( kSet []string, count int, currentStructure map[string]interface{}, newStructure []interface{}, force bool ) interface{} {
    if !force && len(newStructure) == 0 {
        return marshalToInterface( currentStructure )
    }


    if count == len(kSet) - 1 {
        if _, ok := currentStructure[kSet[count]]; !ok {
            currentStructure[kSet[count]] = marshalToInterface( newStructure )
        } else { // if key does exist in current substructure
            if len(newStructure) > 0 {
                currentStructure[kSet[count]] = marshalToInterface( append(
                    currentStructure[kSet[count]].(
                    []interface{}), newStructure... ) )
            }
        }
        return marshalToInterface( currentStructure )
    } else {
        if _, ok := currentStructure[kSet[count]]; !ok {
            currentStructure[kSet[count]] = marshalToInterface(
                addJsonKeyStructure(
                kSet, count + 1, make(map[string]interface{}), newStructure, force ) )
        } else {
            if len(newStructure) > 0 {
                currentStructure[kSet[count]] = marshalToInterface(
                    addJsonKeyStructure(
                    kSet, count + 1, currentStructure[kSet[count]].(
                    map[string]interface{}), newStructure, force ) )
                    // TODO: Catch panics here if they try to put errors in with
                    //    different key depths.
            }
        }
        return marshalToInterface( currentStructure )
    }
}


// A simple Marshal/Unmarshal to force the structure into a JSON-friendly
//    format.
func marshalToInterface( data interface{} ) interface{} {
    jsonIntermediary, err := json.Marshal(data)
    if err != nil {
        LogFatal("marshalToInterface", "Unable to Marshal interface to JSON", err)
    }
    var typeParsedStructure interface{}
    json.Unmarshal([]byte(jsonIntermediary), &typeParsedStructure)
    return typeParsedStructure
}


// Finds where a specific int exists in an []int.
func intInSlice( a int, list []int ) int {
    for i, b := range list {
        if b == a {
            return i
        }
    }
    return -1
}
