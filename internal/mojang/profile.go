// Package mojang implements the Mojang API to query player data.
package mojang

import (
	"bytes"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/http"
)

type Texture struct {
	URL string `json:"url"`
}

type ProfileTextures struct {
	Skin *Texture `json:"SKIN"`
	Cape *Texture `json:"CAPE"`
}

type Profile struct {
	Timestamp   int64           `json:"timestamp"`
	ProfileID   string          `json:"profileId"`
	ProfileName string          `json:"profileName"`
	Textures    ProfileTextures `json:"textures"`
}

type PlayerProperty struct {
	Name         string `json:"name"`
	Signature    string `json:"signature"`
	EncodedValue string `json:"value"`
	Value        Profile
}

// Player represents a Minecraft player's base information.
type Player struct {
	Id             string           `json:"id"`
	Name           string           `json:"name"`
	Properties     []PlayerProperty `json:"properties"`
	ProfileActions []string         `json:"profileActions"`
}

// GetPlayer queries a user's profile data from their user name
func GetPlayer(name string) (*Player, error) {
	url := fmt.Sprintf(
		"https://api.mojang.com/users/profiles/minecraft/%s",
		name,
	)
	req, err := http.NewRequest(
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned status %s", resp.Status)
	}

	var user Player
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	// At this point, we have a UUID and profile name.
	// We must query a second endpoint to grab everything else!
	url = fmt.Sprintf(
		"https://sessionserver.mojang.com/session/minecraft/profile/%s?unsigned=false",
		user.Id,
	)
	req, err = http.NewRequest(
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned status %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	// Now, we have all raw API responses, but we need to b64-decode the properties.
	for i, p := range user.Properties {
		jString, err := b64.StdEncoding.DecodeString(p.EncodedValue)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(jString, &user.Properties[i].Value)
		if err != nil {
			return nil, err
		}
	}

	return &user, nil
}

func (p *Player) GetSkinURL() string {
	return p.Properties[0].Value.Textures.Skin.URL
}

func (p *Player) GetCapeURL() string {
	return p.Properties[0].Value.Textures.Cape.URL
}

func (p *Player) GetSkin() (*image.RGBA, error) {
	resp, err := http.Get(p.GetSkinURL())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status: %s", resp.Status)
	}

	img, err := png.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode PNG: %w", err)
	}

	// Convert the img to an RGBA representation
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)

	return rgba, nil
}

// Returns the players Face as a PNG-Encoded byte array
func (p *Player) GetFace() ([]byte, error) {
	skin, err := p.GetSkin()
	if err != nil {
		return nil, err
	}

	face := skin.SubImage(image.Rect(8, 8, 15, 15)).(*image.RGBA)

	var buf bytes.Buffer
	if err := png.Encode(&buf, face); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
