package render_test

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/render"
)

func ExampleNoteName() {
	fmt.Println(render.NoteName(60))
	fmt.Println(render.NoteName(36))
	fmt.Println(render.NoteName(69))
	// Output:
	// C4
	// C2
	// A4
}

func ExampleFormatBytes() {
	fmt.Println(render.FormatBytes(512))
	fmt.Println(render.FormatBytes(140100))
	fmt.Println(render.FormatBytes(1400000))
	// Output:
	// 512 B
	// 136.8 KB
	// 1.3 MB
}

func ExampleRateName() {
	fmt.Println(render.RateName(0))
	fmt.Println(render.RateName(1))
	fmt.Println(render.RateName(2))
	fmt.Println(render.RateName(99))
	// Output:
	// 36k
	// 18k
	// 9k
	// ?
}
