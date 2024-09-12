package main

import (
	"fmt"
	"syscall/js"
)

type Window struct {
	localStorage LocalStorage
	document     Document

	value js.Value
}

type Document struct {
	getElementById func(id js.Value) js.Value
}

func newDocument() Document {
	document := js.Global().Get("document")

	return Document{
		getElementById: func(id js.Value) js.Value {
			return document.Call("getElementById", id)
		},
	}
}

type LocalStorage struct {
	getItem func(key js.Value) js.Value
	setItem func(key, value js.Value)
}

func newLocalStorage() LocalStorage {
	localStorage := js.Global().Get("localStorage")

	return LocalStorage{
		getItem: func(key js.Value) js.Value {
			return localStorage.Call("getItem", key)
		},
		setItem: func(key, value js.Value) {
			localStorage.Call("setItem", key, value)
		},
	}
}

var window = Window{
	document:     newDocument(),
	localStorage: newLocalStorage(),

	value: js.Global(),
}

func main() {
	// const toggleState = (id) => {
	//     const state = window.localStorage.getItem(id) === 'true'
	//     const el = document.getElementById(id)
	//     el.checked = state
	//     const _toggleState = {
	//         update: (newState) => {
	//             window.localStorage.setItem(id, newState)
	//             el.checked = newState
	//             _toggleState.state = newState
	//         },
	//         state,
	//     }
	//     return _toggleState
	// }
	window.value.Set("_toggleState", js.FuncOf(func(this js.Value, args []js.Value) any {
		id := args[0]
		state := window.localStorage.getItem(id).Equal(js.ValueOf("true"))
		el := window.document.getElementById(id)
		fmt.Println(el.Get("checked"))
		fmt.Println(state)
		el.Set("checked", js.ValueOf(!state))
		window.localStorage.setItem(id, js.ValueOf(!state))

		// toggleState := map[string]interface{}{}

		// toggleState["update"] = js.FuncOf(func(this js.Value, args []js.Value) any {
		// 	newState := args[0]
		// 	fmt.Println(args)
		// 	window.localStorage.setItem(id, newState)
		// 	el.Set("checked", newState)
		// 	toggleState["state"] = newState

		// 	fmt.Println("update")
		// 	return nil
		// })
		// toggleState["state"] = state
		// }
		return map[string]interface{}{
			"update": js.FuncOf(func(this js.Value, args []js.Value) any {
				return nil
			}),
			"state": nil,
		}
	}))

	select {}
}
