package main

import (
	"fmt"

	"gitea.knapp/jacoknapp/scriptorum/internal/util"
)

func main() {
	fmt.Printf("Test 1: %q\n", util.ParseAuthorNameFromTitle("  Last,  First  Extra  "))
	fmt.Printf("Test 2: %q\n", util.ParseAuthorNameFromTitle("Last, "))
}
