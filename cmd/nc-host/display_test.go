package main

import (
	"testing"

	"github.com/askrobots/network-computer/internal/proto"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   proto.InputEvent
		want displayReq
	}{
		{proto.InputEvent{W: 1920, H: 1080, S: 1.5}, displayReq{1920, 1080, 1.5}},
		{proto.InputEvent{W: 1917, H: 1083, S: 1.26}, displayReq{1912, 1080, 1.25}}, // multiples of 8, 5% steps
		{proto.InputEvent{W: 9000, H: 9000, S: 9}, displayReq{2560, 1600, 3}},       // clamped
		{proto.InputEvent{W: 10, H: 10, S: 0}, displayReq{640, 360, 1}},             // floor + default scale
	}
	for _, c := range cases {
		if got := normalize(c.in); got != c.want {
			t.Errorf("normalize(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestBitrateFor(t *testing.T) {
	cases := map[[2]int]string{
		{1280, 720}:  "4000k",  // the base
		{1920, 1080}: "9000k",  // 2.25x the pixels
		{2560, 1600}: "16000k", // capped
		{640, 360}:   "1500k",  // floored
	}
	for wh, want := range cases {
		if got := bitrateFor("4M", wh[0], wh[1]); got != want {
			t.Errorf("bitrateFor(4M, %dx%d) = %s, want %s", wh[0], wh[1], got, want)
		}
	}
}
