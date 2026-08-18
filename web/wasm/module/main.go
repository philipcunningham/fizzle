//go:build js && wasm

// Command module is the Web UI's WASM entry point. It wraps one
// webcore.Session and registers fizzleCore on the JS global: coarse
// methods that take and return plain JS values, with every result
// wrapped in an {ok, value} or {ok, error} envelope. A Go panic is
// recovered into an envelope; it never crosses the boundary raw.
package main

import (
	"fmt"
	"syscall/js"

	"github.com/philipcunningham/fizzle/pkg/logger"
	"github.com/philipcunningham/fizzle/pkg/webcore"
)

func snapshotJS(s webcore.Snapshot) map[string]any {
	var disk any
	if s.Disk != nil {
		files := make([]any, 0, len(s.Disk.Files))
		for _, f := range s.Disk.Files {
			entry := map[string]any{
				"name":      f.Name,
				"type":      f.Type,
				"sizeBytes": f.SizeBytes,
			}
			if f.Params != nil {
				params := make(map[string]any, len(f.Params))
				for k, v := range f.Params {
					params[k] = v
				}
				entry["params"] = params
			}
			if f.Voice != nil {
				entry["voice"] = voiceDetailJS(f.Voice)
			}
			files = append(files, entry)
		}
		diskMap := map[string]any{
			"label":         s.Disk.Label,
			"usedBytes":     s.Disk.UsedBytes,
			"capacityBytes": s.Disk.CapacityBytes,
			"disks":         s.Disk.Disks,
			"files":         files,
		}
		if s.Disk.MissingDisk != 0 {
			diskMap["missingDisk"] = s.Disk.MissingDisk
		}
		if s.Disk.Instrument != nil {
			diskMap["instrument"] = instrumentJS(s.Disk.Instrument)
		}
		disk = diskMap
	}
	return map[string]any{
		"revision": s.Revision,
		"disk":     disk,
		"canUndo":  s.CanUndo,
		"canRedo":  s.CanRedo,
	}
}

func instrumentJS(inst *webcore.InstrumentSnapshot) map[string]any {
	banks := make([]any, len(inst.Banks))
	for i, b := range inst.Banks {
		areas := make([]any, len(b.Areas))
		for j, a := range b.Areas {
			areas[j] = map[string]any{
				"voiceSlot":   a.VoiceSlot,
				"voiceName":   a.VoiceName,
				"keyLow":      a.KeyLow,
				"keyHigh":     a.KeyHigh,
				"root":        a.Root,
				"velLow":      a.VelLow,
				"velHigh":     a.VelHigh,
				"midiChannel": a.MidiChannel,
				"output":      a.Output,
				"outputLabel": a.OutputLabel,
				"volume":      a.Volume,
			}
		}
		banks[i] = map[string]any{"name": b.Name, "areas": areas}
	}
	voices := make([]any, len(inst.Voices))
	for i, v := range inst.Voices {
		entry := map[string]any{
			"slot": v.Slot, "name": v.Name, "referenced": v.Referenced,
			"sharesAudio": v.SharesAudio, "audioKey": v.AudioKey,
		}
		if v.Params != nil {
			params := make(map[string]any, len(v.Params))
			for k, val := range v.Params {
				params[k] = val
			}
			entry["params"] = params
		}
		if v.Voice != nil {
			entry["voice"] = voiceDetailJS(v.Voice)
		}
		voices[i] = entry
	}
	out := map[string]any{"fileName": inst.FileName, "banks": banks, "voices": voices}
	if inst.Effects != nil {
		matrix := make([]any, len(inst.Effects.Matrix))
		for i, row := range inst.Effects.Matrix {
			cells := make([]any, len(row))
			for j, v := range row {
				cells[j] = v
			}
			matrix[i] = cells
		}
		out["effects"] = map[string]any{"bendRange": inst.Effects.BendRange, "matrix": matrix}
	}
	return out
}

func envelopeJS(e webcore.EnvelopeSnapshot) map[string]any {
	rates := make([]any, len(e.Rates))
	for i, v := range e.Rates {
		rates[i] = v
	}
	stops := make([]any, len(e.Stops))
	for i, v := range e.Stops {
		stops[i] = v
	}
	return map[string]any{"sustain": e.Sustain, "end": e.End, "rates": rates, "stops": stops}
}

func voiceDetailJS(d *webcore.VoiceDetail) map[string]any {
	loops := make([]any, len(d.Loops))
	for i, l := range d.Loops {
		loops[i] = map[string]any{"start": l.Start, "end": l.End, "xf": l.XF, "tm": l.Tm}
	}
	return map[string]any{
		"frames":      d.Frames,
		"sampleRate":  d.SampleRate,
		"genStart":    d.GenStart,
		"genEnd":      d.GenEnd,
		"loopSustain": d.LoopSustain,
		"loopRelease": d.LoopRelease,
		"loops":       loops,
		"dca":         envelopeJS(d.Dca),
		"dcf":         envelopeJS(d.Dcf),
	}
}

func intArgs(v js.Value) []int {
	out := make([]int, v.Length())
	for i := range out {
		out[i] = v.Index(i).Int()
	}
	return out
}

// bytesArg copies a Uint8Array argument into Go memory.
func bytesArg(v js.Value) []byte {
	data := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(data, v)
	return data
}

// filesArg copies a {path: Uint8Array} object into a Go file map, the
// boundary shape for folder imports.
func filesArg(v js.Value) map[string][]byte {
	files := map[string][]byte{}
	keys := js.Global().Get("Object").Call("keys", v)
	for i := 0; i < keys.Length(); i++ {
		name := keys.Index(i).String()
		files[name] = bytesArg(v.Get(name))
	}
	return files
}

func schemaJS() []any {
	fields := webcore.Schema()
	out := make([]any, 0, len(fields))
	for _, f := range fields {
		entry := map[string]any{
			"id":    f.ID,
			"label": f.Label,
			"group": f.Group,
			"kind":  f.Kind,
			"min":   f.Min,
			"max":   f.Max,
		}
		if len(f.Options) > 0 {
			opts := make([]any, 0, len(f.Options))
			for _, o := range f.Options {
				opts = append(opts, o)
			}
			entry["options"] = opts
		}
		out = append(out, entry)
	}
	return out
}

func okEnvelope(value any) map[string]any {
	return map[string]any{"ok": true, "value": value}
}

func errEnvelope(cerr *webcore.Error) map[string]any {
	e := map[string]any{
		"code":    cerr.Code,
		"message": cerr.Message,
	}
	if cerr.Item != "" {
		e["item"] = cerr.Item
	}
	if cerr.Detail != "" {
		e["detail"] = cerr.Detail
	}
	return map[string]any{"ok": false, "error": e}
}

// method wraps a handler with panic recovery so the boundary always
// returns an envelope.
func method(fn func(args []js.Value) map[string]any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) (result any) {
		defer func() {
			if r := recover(); r != nil {
				result = errEnvelope(&webcore.Error{
					Code:    "panic",
					Message: fmt.Sprintf("core panic: %v", r),
				})
			}
		}()
		return fn(args)
	})
}

func main() {
	logger.Init(false)
	session := webcore.NewSession()

	core := map[string]any{}
	core["snapshot"] = method(func(_ []js.Value) map[string]any {
		return okEnvelope(snapshotJS(session.Snapshot()))
	})
	core["newDisk"] = method(func(args []js.Value) map[string]any {
		snap, cerr := session.NewDisk(args[0].String())
		if cerr != nil {
			return errEnvelope(cerr)
		}
		return okEnvelope(snapshotJS(snap))
	})
	core["openImage"] = method(func(args []js.Value) map[string]any {
		data := make([]byte, args[0].Get("length").Int())
		js.CopyBytesToGo(data, args[0])
		snap, cerr := session.OpenImage(data)
		if cerr != nil {
			return errEnvelope(cerr)
		}
		return okEnvelope(snapshotJS(snap))
	})
	core["schema"] = method(func(_ []js.Value) map[string]any {
		return okEnvelope(schemaJS())
	})
	core["undo"] = method(func(_ []js.Value) map[string]any {
		snap, cerr := session.Undo()
		if cerr != nil {
			return errEnvelope(cerr)
		}
		return okEnvelope(snapshotJS(snap))
	})
	core["redo"] = method(func(_ []js.Value) map[string]any {
		snap, cerr := session.Redo()
		if cerr != nil {
			return errEnvelope(cerr)
		}
		return okEnvelope(snapshotJS(snap))
	})
	core["beginGesture"] = method(func(_ []js.Value) map[string]any {
		session.BeginGesture()
		return okEnvelope(snapshotJS(session.Snapshot()))
	})
	core["commitGesture"] = method(func(_ []js.Value) map[string]any {
		// The envelope carries whether the gesture landed an entry, so a
		// press and release with no movement doesn't dirty the document.
		landed := session.CommitGesture()
		snap := snapshotJS(session.Snapshot())
		snap["gestureLanded"] = landed
		return okEnvelope(snap)
	})
	snapOrErr := func(snap webcore.Snapshot, cerr *webcore.Error) map[string]any {
		if cerr != nil {
			return errEnvelope(cerr)
		}
		return okEnvelope(snapshotJS(snap))
	}
	core["setAreaField"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetAreaField(args[0].Int(), args[1].Int(), args[2].String(), args[3].Int()))
	})
	core["renameBank"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.RenameBank(args[0].Int(), args[1].String()))
	})
	core["swapAreas"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SwapAreas(args[0].Int(), args[1].Int(), args[2].Int()))
	})
	core["deleteArea"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.DeleteArea(args[0].Int(), args[1].Int()))
	})
	core["addArea"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.AddArea(args[0].Int(), args[1].Int()))
	})
	core["duplicateArea"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.DuplicateArea(args[0].Int(), args[1].Int()))
	})
	core["mapVoice"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.MapVoice(args[0].Int()))
	})
	core["setEffectCell"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetEffectCell(args[0].Int(), args[1].Int(), args[2].Int()))
	})
	core["setBendRange"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetBendRange(args[0].Int()))
	})
	auditionJS := func(a *webcore.Audition, cerr *webcore.Error) map[string]any {
		if cerr != nil {
			return errEnvelope(cerr)
		}
		raw := make([]byte, len(a.PCM)*2)
		for i, v := range a.PCM {
			raw[i*2] = byte(uint16(v))
			raw[i*2+1] = byte(uint16(v) >> 8)
		}
		out := js.Global().Get("Uint8Array").New(len(raw))
		js.CopyBytesToJS(out, raw)
		return okEnvelope(map[string]any{
			"sampleRate": a.SampleRate,
			"root":       a.Root,
			"pcm":        out,
		})
	}
	core["auditionSlot"] = method(func(args []js.Value) map[string]any {
		return auditionJS(session.AuditionSlot(args[0].Int()))
	})
	core["exportImage"] = method(func(_ []js.Value) map[string]any {
		data, cerr := session.ExportImage()
		if cerr != nil {
			return errEnvelope(cerr)
		}
		out := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(out, data)
		return okEnvelope(out)
	})
	core["exportImageAt"] = method(func(args []js.Value) map[string]any {
		data, cerr := session.ExportImageAt(args[0].Int())
		if cerr != nil {
			return errEnvelope(cerr)
		}
		out := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(out, data)
		return okEnvelope(out)
	})
	core["loadFzf"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.LoadFZF(bytesArg(args[0])))
	})
	core["addVoice"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.AddVoice(bytesArg(args[0])))
	})
	core["addBank"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.AddBank(bytesArg(args[0]), args[1].Int()))
	})
	core["importWavToInstrument"] = method(func(args []js.Value) map[string]any {
		// #nosec G115 -- webcore validates the rate.
		return snapOrErr(session.ImportWAVToInstrument(args[0].String(), bytesArg(args[1]), uint32(args[2].Int()), args[3].String()))
	})
	core["openImagePair"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.OpenImagePair(bytesArg(args[0]), bytesArg(args[1])))
	})
	sfzResultJS := func(res webcore.SFZResult, cerr *webcore.Error) map[string]any {
		if cerr != nil {
			return errEnvelope(cerr)
		}
		return okEnvelope(map[string]any{
			"snapshot": snapshotJS(res.Snapshot),
			"rate":     res.Rate,
		})
	}
	core["importSfz"] = method(func(args []js.Value) map[string]any {
		return sfzResultJS(session.ImportSFZ(
			filesArg(args[0]), args[1].String(), args[2].Int(), args[3].Bool(), args[4].Bool(), args[5].String()))
	})
	core["importWavFolder"] = method(func(args []js.Value) map[string]any {
		return sfzResultJS(session.ImportWAVFolder(
			filesArg(args[0]), args[1].Int(), args[2].Bool(), args[3].String()))
	})
	core["estimateImport"] = method(func(args []js.Value) map[string]any {
		// #nosec G115 -- webcore validates the rate.
		est, cerr := session.EstimateImport(filesArg(args[0]), uint32(args[1].Int()), args[2].String())
		if cerr != nil {
			return errEnvelope(cerr)
		}
		fitsAt := make([]any, 0, len(est.FitsAtRates))
		for _, r := range est.FitsAtRates {
			fitsAt = append(fitsAt, r)
		}
		return okEnvelope(map[string]any{
			"bytes":       est.Bytes,
			"seconds":     est.Seconds,
			"roomSeconds": est.RoomSeconds,
			"verdict":     est.Verdict,
			"reason":      est.Reason,
			"anyStereo":   est.AnyStereo,
			"overCapFile": est.OverCapFile,
			"fileSeconds": est.FileSeconds,
			"capSeconds":  est.CapSeconds,
			"fitsAtRates": fitsAt,
		})
	})
	core["setDebug"] = method(func(args []js.Value) map[string]any {
		// Core logs reach the console through the stderr shim; this is
		// the CLI debug flag's analogue (E4).
		logger.Init(args[0].Bool())
		return okEnvelope(nil)
	})
	core["setSlotParamNumber"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetSlotParamNumber(args[0].Int(), args[1].String(), args[2].Int()))
	})
	core["setSlotParamOption"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetSlotParamOption(args[0].Int(), args[1].String(), args[2].String()))
	})
	core["setSlotGeneration"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetSlotGeneration(args[0].Int(), args[1].Int(), args[2].Int()))
	})
	core["setSlotLoop"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetSlotLoop(args[0].Int(), args[1].Int(), args[2].Int(), args[3].Int()))
	})
	core["setSlotLoopAttr"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetSlotLoopAttr(args[0].Int(), args[1].Int(), args[2].Int(), args[3].Int()))
	})
	core["setSlotLoopSelect"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetSlotLoopSelect(args[0].Int(), args[1].Int(), args[2].Int()))
	})
	core["setSlotEnvelope"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.SetSlotEnvelope(
			args[0].Int(), args[1].String(), args[2].Int(), args[3].Int(),
			intArgs(args[4]), intArgs(args[5])))
	})
	core["renameVoiceSlot"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.RenameVoiceSlot(args[0].Int(), args[1].String()))
	})
	core["renameDisk"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.RenameDisk(args[0].String()))
	})
	core["closeDisk"] = method(func(_ []js.Value) map[string]any {
		return snapOrErr(session.Close())
	})
	core["deleteFile"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.DeleteFile(args[0].String()))
	})
	core["newInstrument"] = method(func(args []js.Value) map[string]any {
		return snapOrErr(session.NewInstrument(args[0].String()))
	})
	core["extractFile"] = method(func(args []js.Value) map[string]any {
		data, cerr := session.ExtractFile(args[0].String())
		if cerr != nil {
			return errEnvelope(cerr)
		}
		out := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(out, data)
		return okEnvelope(out)
	})
	core["extractVoiceSlot"] = method(func(args []js.Value) map[string]any {
		data, name, cerr := session.ExtractVoiceSlot(args[0].Int(), args[1].String())
		if cerr != nil {
			return errEnvelope(cerr)
		}
		out := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(out, data)
		return okEnvelope(map[string]any{"name": name, "bytes": out})
	})
	core["slotPeaks"] = method(func(args []js.Value) map[string]any {
		pairs, cerr := session.SlotPeaks(args[0].Int(), args[1].Int(), args[2].Int(), args[3].Int())
		if cerr != nil {
			return errEnvelope(cerr)
		}
		raw := make([]byte, len(pairs)*2)
		for i, v := range pairs {
			raw[i*2] = byte(uint16(v))
			raw[i*2+1] = byte(uint16(v) >> 8)
		}
		out := js.Global().Get("Uint8Array").New(len(raw))
		js.CopyBytesToJS(out, raw)
		return okEnvelope(out)
	})

	js.Global().Set("fizzleCore", js.ValueOf(core))
	if ready := js.Global().Get("onFizzleReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}

	// Block forever; the worker owns the module's lifetime.
	select {}
}
