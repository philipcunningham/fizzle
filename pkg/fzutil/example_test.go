package fzutil_test

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/fzutil"
)

func ExampleVoiceName() {
	fmt.Println(fzutil.VoiceName("samples/kick drum.wav"))
	fmt.Println(fzutil.VoiceName("My-808_Snare.WAV"))
	fmt.Println(fzutil.VoiceName("HOOVER"))
	// Output:
	// KICK DRUM
	// MY 808 SNARE
	// HOOVER
}
