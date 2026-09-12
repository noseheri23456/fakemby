package types

import "encoding/json"

// MarshalJSON keeps collections safe for clients which call length/filter on
// them without guarding against missing or null values, including partial DTOs.
func (d BaseItemDto) MarshalJSON() ([]byte, error) {
	type wire BaseItemDto
	if d.GenreItems == nil {
		d.GenreItems = []NameIdPair{}
	}
	if d.Genres == nil {
		d.Genres = []string{}
	}
	if d.Studios == nil {
		d.Studios = []NameIdPair{}
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if d.Taglines == nil {
		d.Taglines = []string{}
	}
	if d.People == nil {
		d.People = []PersonInfo{}
	}
	if d.ImageTags == nil {
		d.ImageTags = map[string]string{}
	}
	if d.ProviderIds == nil {
		d.ProviderIds = map[string]string{}
	}
	if d.BackdropImageTags == nil {
		d.BackdropImageTags = []string{}
	}
	if d.ParentBackdropImageTags == nil {
		d.ParentBackdropImageTags = []string{}
	}
	if d.LockedFields == nil {
		d.LockedFields = []string{}
	}
	if d.Subviews == nil {
		d.Subviews = []string{}
	}
	if d.MediaSources == nil {
		d.MediaSources = []MediaSourceDto{}
	}
	if d.RemoteTrailers == nil {
		d.RemoteTrailers = []ExternalUrl{}
	}
	if d.ExternalUrls == nil {
		d.ExternalUrls = []ExternalUrl{}
	}
	if d.UserData == nil {
		d.UserData = &UserItemDataDto{}
	}
	return json.Marshal(wire(d))
}
func (d MediaSourceDto) MarshalJSON() ([]byte, error) {
	type wire MediaSourceDto
	if d.MediaStreams == nil {
		d.MediaStreams = []MediaStreamDto{}
	}
	if d.Formats == nil {
		d.Formats = []string{}
	}
	if d.RequiredHttpHeaders == nil {
		d.RequiredHttpHeaders = map[string]string{}
	}
	return json.Marshal(wire(d))
}
