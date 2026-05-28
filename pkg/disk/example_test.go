package disk_test

import (
	"fmt"

	"github.com/philipcunningham/fizzle/pkg/disk"
)

func ExamplePadLabel() {
	label := disk.PadLabel("HOOVER")
	fmt.Printf("%q\n", string(label[:]))
	// Output:
	// "HOOVER      "
}

func ExampleTrimPadded() {
	padded := []byte("HOOVER      ")
	fmt.Println(disk.TrimPadded(padded))
	// Output:
	// HOOVER
}

func ExampleSectorsNeeded() {
	fmt.Println(disk.SectorsNeeded(1))
	fmt.Println(disk.SectorsNeeded(1024))
	fmt.Println(disk.SectorsNeeded(1025))
	// Output:
	// 1
	// 1
	// 2
}

func ExampleFileType_String() {
	fmt.Println(disk.TypeVoice)
	fmt.Println(disk.TypeFullDump)
	// Output:
	// Voice
	// Full Dump
}

func ExampleRateIndexFor() {
	idx, ok := disk.RateIndexFor(36000)
	fmt.Printf("36000 Hz: index=%d ok=%v\n", idx, ok)

	idx, ok = disk.RateIndexFor(18000)
	fmt.Printf("18000 Hz: index=%d ok=%v\n", idx, ok)

	_, ok = disk.RateIndexFor(44100)
	fmt.Printf("44100 Hz: ok=%v\n", ok)
	// Output:
	// 36000 Hz: index=0 ok=true
	// 18000 Hz: index=1 ok=true
	// 44100 Hz: ok=false
}

func ExampleSampleRate() {
	fmt.Println(disk.SampleRate(0))
	fmt.Println(disk.SampleRate(1))
	fmt.Println(disk.SampleRate(2))
	fmt.Println(disk.SampleRate(99))
	// Output:
	// 36000
	// 18000
	// 9000
	// 0
}
