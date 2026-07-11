package usagelogs

import (
	"reflect"
	"testing"
)

func TestLooksLikeAuthIndex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "live file seed", value: "39a7982984e321e5", want: true},
		{name: "orphan id seed", value: "69e8946f1ffc2d23", want: true},
		{name: "uppercase hex", value: "69E8946F1FFC2D23", want: true},
		{name: "email label", value: "asherandersenloqv@outlook.com", want: false},
		{name: "display name", value: "Codex 主渠道", want: false},
		{name: "too short", value: "39a7982984e321e", want: false},
		{name: "too long", value: "39a7982984e321e5a", want: false},
		{name: "non hex", value: "gggggggggggggggg", want: false},
		{name: "empty", value: "", want: false},
		{name: "spaces", value: "  39a7982984e321e5  ", want: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeAuthIndex(tc.value); got != tc.want {
				t.Fatalf("looksLikeAuthIndex(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestChannelFilterSelectorsTreatsOrphanAuthIndexAsAuthIndex(t *testing.T) {
	t.Parallel()

	// Live auth currently uses the file: seed index; historical rows still use
	// the id: seed index for the same OAuth email label.
	liveIndex := "39a7982984e321e5"
	orphanIndex := "69e8946f1ffc2d23"
	label := "asherandersenloqv@outlook.com"

	authIndexChannelMap := map[string]string{liveIndex: label}
	authMetaByIndex := map[string]authChannelMeta{
		liveIndex: {label: label, provider: "xai", authType: "oauth"},
	}

	// Selecting the orphan facet value must stay on AuthIndexes. The previous
	// bug fell through to ChannelNames and queried channel_name = <hash>.
	authIndexes, channelNames, _ := channelFilterSelectors(
		[]string{orphanIndex},
		nil,
		authIndexChannelMap,
		nil,
		authMetaByIndex,
	)
	if !reflect.DeepEqual(authIndexes, []string{orphanIndex}) {
		t.Fatalf("authIndexes = %#v, want [%s]", authIndexes, orphanIndex)
	}
	if len(channelNames) != 0 {
		t.Fatalf("channelNames = %#v, want empty", channelNames)
	}

	// Live index still resolves normally.
	authIndexes, channelNames, _ = channelFilterSelectors(
		[]string{liveIndex},
		nil,
		authIndexChannelMap,
		nil,
		authMetaByIndex,
	)
	if !reflect.DeepEqual(authIndexes, []string{liveIndex}) {
		t.Fatalf("live authIndexes = %#v, want [%s]", authIndexes, liveIndex)
	}
	if len(channelNames) != 0 {
		t.Fatalf("live channelNames = %#v, want empty", channelNames)
	}

	// Email/display labels still use the legacy channel_name path (and may also
	// expand to live auth indexes via authIndexChannelMap label matching).
	authIndexes, channelNames, _ = channelFilterSelectors(
		[]string{label},
		map[string]string{label: label},
		authIndexChannelMap,
		nil,
		authMetaByIndex,
	)
	if !reflect.DeepEqual(authIndexes, []string{liveIndex}) {
		t.Fatalf("label authIndexes = %#v, want [%s]", authIndexes, liveIndex)
	}
	if !reflect.DeepEqual(channelNames, []string{label}) {
		t.Fatalf("label channelNames = %#v, want [%s]", channelNames, label)
	}
}
