package main

import (
	"fmt"
	"reflect"
	"github.com/anacrolix/torrent/storage"
)

func main() {
	check("ClientImpl", reflect.TypeOf((*storage.ClientImpl)(nil)).Elem())
	check("TorrentImpl", reflect.TypeOf((*storage.TorrentImpl)(nil)).Elem())
	check("PieceImpl", reflect.TypeOf((*storage.PieceImpl)(nil)).Elem())
}

func check(name string, t reflect.Type) {
	fmt.Printf("--- %s ---\n", name)
	fmt.Printf("Kind: %s\n", t.Kind())
	if t.Kind() == reflect.Interface {
		fmt.Printf("Methods: %d\n", t.NumMethod())
		for i := 0; i < t.NumMethod(); i++ {
			m := t.Method(i)
			fmt.Printf("  Method: %s %s\n", m.Name, m.Type)
		}
	} else if t.Kind() == reflect.Struct {
		fmt.Printf("Fields: %d\n", t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fmt.Printf("  Field: %s %s\n", f.Name, f.Type)
		}
	}
}
