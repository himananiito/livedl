
package objs

import (
	"fmt"
	"encoding/json"
)

func PrintAsJson(data interface{}) {
	json, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	fmt.Println(string(json))
}
func Find(intf interface{}, keylist... string) (res interface{}, ok bool) {
	res = intf
	if len(keylist) == 0 {
		ok = true
		return
	}
	for i, k := range keylist {
		var test bool
		//var obj map[string]interface{}
		switch res.(type) {
		case map[string]interface{}:
			res, test = res.(map[string]interface{})[k]
			if (! test) {
				ok = false
				return
			}
		case []interface{}:
			for _, o := range res.([]interface{}) {
				_res, _ok := Find(o, keylist[i:]...)
				if _ok {
					res = _res
					ok = _ok
					return
				}
			}
		}
	}
	ok = true
	return
}
func FindFloat64(intf interface{}, keylist... string) (res float64, ok bool) {
	val, ok := Find(intf, keylist...)
	if !ok {
		return
	}
	res, ok = val.(float64)
	return
}
func FindString(intf interface{}, keylist... string) (res string, ok bool) {
	val, ok := Find(intf, keylist...)
	if !ok {
		return
	}
	res, ok = val.(string)
	return
}
func FindBool(intf interface{}, keylist... string) (res bool, ok bool) {
	val, ok := Find(intf, keylist...)
	if !ok {
		return
	}
	res, ok = val.(bool)
	return
}
func FindArray(intf interface{}, keylist... string) (res []interface{}, ok bool) {
	val, ok := Find(intf, keylist...)
	if !ok {
		return
	}
	res, ok = val.([]interface{})
	return
}

