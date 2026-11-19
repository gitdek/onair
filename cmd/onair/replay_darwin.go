//go:build darwin

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gitdek/onair/internal/archive"
	"github.com/gitdek/onair/internal/live"
	"github.com/gitdek/onair/internal/localmodel"
	"github.com/gitdek/onair/internal/replay"
)

// replayCommand plays a transcript with word times, in the shape Whisper
// writes, through the live side with the real archive and the real local
// models, and keeps what the panel showed and when, in On Air's own
// folder, where only its owner may read it. It prints counts and where it
// kept them, never what was said or shown.
func replayCommand(args []string) error {
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	speed := flags.Float64("speed", 4, "how many times as fast as the meeting the replay's clock runs")
	lose := flags.Float64("lose", replay.Measured.Lose, "the share of words recognition never hears")
	label := flags.String("as", "replay", "the name of this run, which its files are kept under")
	models := flags.Bool("models", true, "ask the local models, as with \"Ask a local model about the call\" chosen")
	report := flags.String("report", "", "write the timeline of runs kept before, named and separated by commas, instead of playing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: onair replay [-as NAME] [-speed N] [-lose SHARE] [-models=false] [-report NAME,NAME] TRANSCRIPT.json")
	}
	if *label == "" || strings.ContainsAny(*label, `/\`) || strings.HasPrefix(*label, ".") {
		return fmt.Errorf("a run's name is a plain word, not %q", *label)
	}
	path := flags.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	words, err := replay.ReadWhisper(f)
	f.Close()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	support := filepath.Join(home, "Library", "Application Support", "On Air")
	stem := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".json"), ".whisper")
	dir := filepath.Join(support, "replays", stem)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// A recording's file is named for when it began.
	start, err := time.ParseInLocation("2006-01-02 15.04.05", strings.TrimSuffix(stem, " On Air"), time.Local)
	if err != nil {
		start = time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local)
	}
	client, err := archive.New(archive.Door)
	if err != nil {
		return err
	}
	clock := replay.NewClock(start, *speed)
	store := &replay.Store{Dir: filepath.Join(support, "replays", "kept")}
	if *report != "" {
		return writeReport(dir, stem, strings.Split(*report, ","), words, start, replay.Archive{Real: client, Clock: clock, Store: store})
	}
	run := replay.Run{Words: words, Start: start, Speed: *speed, Heard: replay.Measured, Config: live.Defaults,
		Archive: replay.Archive{Real: client, Clock: clock, Store: store}}
	run.Heard.Lose = *lose
	if *models {
		python := filepath.Join(support, "models", "venv", "bin", "python")
		writer, judge := localmodel.NewWriter(python), localmodel.New(python)
		defer writer.Close()
		defer judge.Close()
		run.Writer = &replay.Writer{Real: writer, Clock: clock, Store: store}
		run.Judge = &replay.Judge{Real: judge, Clock: clock, Store: store}
	}
	run.Progress = func(done, of time.Duration) {
		fmt.Printf("%s of %s, %d asked for real, %d answered from what was kept\n", done.Round(time.Second), of.Round(time.Second), store.Asked.Load(), store.Kept.Load())
	}
	began := time.Now()
	shown := run.Play(clock)
	out := filepath.Join(dir, *label+".jsonl")
	file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := replay.Write(file, shown); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := writeReport(dir, stem, []string{*label}, words, start, replay.Archive{Real: client, Clock: clock, Store: store}); err != nil {
		return err
	}
	panels := 0
	for _, s := range shown {
		if s.Panel != nil {
			panels++
		}
	}
	fmt.Printf("%d words played in %s; %d panels shown, %d lines logged; %d asked for real, %d answered from what was kept\nkept in %s\n",
		len(words), time.Since(began).Round(time.Second), panels, len(shown)-panels, store.Asked.Load(), store.Kept.Load(), out)
	return nil
}

// writeReport writes the timeline of the runs named, as kept in dir, to a
// file of its own there, which only its owner may read, and says where.
func writeReport(dir, stem string, names []string, words []replay.Word, start time.Time, door replay.Archive) error {
	var runs []replay.Played
	for _, name := range names {
		f, err := os.Open(filepath.Join(dir, strings.TrimSpace(name)+".jsonl"))
		if err != nil {
			return err
		}
		shown, err := replay.Read(f)
		f.Close()
		if err != nil {
			return err
		}
		runs = append(runs, replay.Played{Name: strings.TrimSpace(name), Shown: shown})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lib, err := door.Library(ctx)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, strings.Join(names, " and ")+".txt")
	file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	title := fmt.Sprintf("On Air replay of %s: %s", stem, strings.Join(names, " and "))
	if err := replay.Report(file, title, words, start, runs, replay.Mentions(words, live.Names(lib))); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Printf("the timeline is in %s\n", out)
	return nil
}
