package main

import (
	"fmt"
	"reflect"

	"github.com/anacrolix/torrent/storage"
)

func main() {
	printInterface("TorrentImpl", reflect.TypeOf((*storage.TorrentImpl)(nil)).Elem())
	printInterface("PieceImpl", reflect.TypeOf((*storage.PieceImpl)(nil)).Elem())
}

func printInterface(name string, t reflect.Type) {
	fmt.Printf("Interface: %s\n", name)
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		fmt.Printf("  %s%s\n", m.Name, m.Type)
	}
}
