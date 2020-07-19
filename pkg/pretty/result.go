// Copyright 2017-2020 misatos.angel@gmail.com.  All rights reserved.

package pretty

import (
	"github.com/misatosangel/soku-cardinfo/pkg/card-info"
	"github.com/misatosangel/soku-net-checker/pkg/checker"
)

type Card struct {
	Code  uint16 `json:"code,omitempty"`
	Name  string `json:"name,omitempty"`
	Type  string `json:"type,omitempty"`
	Cost  uint16 `json:"cost,omitempty"`
	Count uint16 `json:"count,omitempty"`
}

type CharInfo struct {
	CharCode    uint8  `json:"char_num,omitempty"`
	PaletteCode uint8  `json:"profile_num,omitempty"`
	DeckCode    uint8  `json:"deck_num,omitempty"`
	Character   string `json:"character,omitempty"`
	DeckName    string `json:"deck_name,omitempty"`
	Deck        []Card `json:"deck,omitempty"`
}

type GameInfo struct {
	P1        *CharInfo `json:"player1,omitempty"`
	P2        *CharInfo `json:"player2,omitempty"`
	LevelCode uint8     `json:"level_num,omitempty"`
	TrackCode uint8     `json:"music_track_num,omitempty"`
	Level     string    `json:"level_num,omitempty"`
	Track     string    `json:"music_track_num,omitempty"`
	RNG       uint32    `json:"rng_seed,omitempty"`
	Count     byte      `json:"game_num,omitempty"`
}

type Result struct {
	Address   string    `json:"address,omitempty"`
	Status    string    `json:"status,omitempty"`
	Error     string    `json:"error,omitempty"`
	Version   string    `json:"version,omitempty"`
	Opponent  string    `json:"opponent,omitempty"`
	Spectate  string    `json:"spectate,omitempty"`
	Profiles  []string  `json:"profiles,omitempty"`
	SpecChain []string  `json:"spec_chain,omitempty"`
	Game      *GameInfo `json:"game,omitempty"`
}

func MarkupResult(raw checker.CheckResult, cards cardinfo.AllCards) Result {
	var spec string
	switch raw.Spectate {
	case 'y':
		spec = "yes"
	case 'n':
		spec = "no"
	default:
		spec = "unknown"
	}

	return Result{
		Address:   raw.Address,
		Status:    raw.Status,
		Error:     raw.Error,
		Version:   raw.Version,
		Opponent:  raw.Opponent,
		Profiles:  raw.Profiles,
		SpecChain: raw.Spec,
		Spectate:  spec,
		Game:      MarkupGame(raw.CurGame, cards),
	}
}

func MarkupGame(raw *checker.GameInfo, cards cardinfo.AllCards) *GameInfo {
	if raw == nil {
		return nil
	}
	var rng uint32
	if len(raw.RNG) == 4 {
		rng = uint32(raw.RNG[0])
		rng <<= 8
		rng += uint32(raw.RNG[1])
		rng <<= 8
		rng += uint32(raw.RNG[2])
		rng <<= 8
		rng += uint32(raw.RNG[3])
	}
	var p1Info *CharInfo
	var p2Info *CharInfo
	if len(raw.Players) > 0 {
		p1Info = MarkupCharInfo(raw.Players[0], cards)
		if len(raw.Players) > 1 {
			p2Info = MarkupCharInfo(raw.Players[1], cards)
		}
	}
	return &GameInfo{
		P1:        p1Info,
		P2:        p2Info,
		LevelCode: uint8(raw.Lvl),
		TrackCode: uint8(raw.Track),
		Level:     checker.GetLevelName(raw.Lvl),
		Track:     checker.GetMusicName(raw.Track),
		RNG:       rng,
		Count:     raw.Count,
	}
}

func MarkupCharInfo(raw *checker.CharInfo, cards cardinfo.AllCards) *CharInfo {
	if raw == nil {
		return nil
	}
	charName := raw.GetCharName()
	deck, err := cards.NewDeck(charName, raw.DeckInfo)
	outDeck := make([]Card, 0, 20)
	if err == nil {
		for _, count := range deck.Cards {
			c := count.Card
			outDeck = append(outDeck, Card{
				Code:  c.Code,
				Name:  c.Name,
				Type:  c.Type,
				Cost:  c.Cost,
				Count: count.Count,
			})
		}
	}
	return &CharInfo{
		CharCode:    uint8(raw.Char),
		PaletteCode: raw.Palette,
		DeckCode:    uint8(raw.SelectedDeck),
		Character:   charName,
		DeckName:    raw.GetDeckName(),
		Deck:        outDeck,
	}
}
