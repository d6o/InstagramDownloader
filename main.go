package main

import (
	"fmt"
	"github.com/ahmdrz/goinsta"
	"github.com/ahmdrz/goinsta/response"
	"github.com/buger/goterm"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type (
	Tray response.TrayResponse
	User response.User
	Item response.Item

	MultiInsta struct {
		Main         *goinsta.Instagram
		Alternatives []*goinsta.Instagram
	}

	media struct {
		URL       string
		Username  string
		Timestamp int64
		Path      string
	}
)

const (
	Images = "images"
	Videos = "videos"
)

func (t *Tray) Images() []*media {
	mediaList := []*media{}

	for _, val := range t.Tray {
		for _, me := range val.Media {
			for _, image := range me.ImageVersions2.Candidates {
				timePath := time.Unix(me.DeviceTimestamp, 0).Format("2006/01/02")

				path := fmt.Sprintf("%s/%s/%d/%d", Images, timePath, image.Width, image.Height)

				mediaList = append(mediaList, &media{
					URL:       image.URL,
					Username:  me.User.Username,
					Timestamp: me.DeviceTimestamp,
					Path:      path,
				})
			}
		}
	}
	return mediaList
}

func (t *Tray) Videos() []*media {
	mediaList := []*media{}

	for _, val := range t.Tray {
		for _, me := range val.Media {
			for _, video := range me.VideoVersions {
				timePath := time.Unix(me.DeviceTimestamp, 0).Format("2006/01/02")

				mediaList = append(mediaList, &media{
					URL:       video.URL,
					Username:  me.User.Username,
					Timestamp: me.DeviceTimestamp,
					Path:      fmt.Sprintf("%s/%s/%d/%d/%d", Videos, timePath, video.Width, video.Height, video.Type),
				})
			}
		}
	}
	return mediaList
}

func (t *Tray) Media() []*media {
	mediaList := []*media{}

	mediaList = append(mediaList, t.Images()...)
	mediaList = append(mediaList, t.Videos()...)

	return mediaList
}

func (u *User) Media(insta *MultiInsta, lastCheck string) ([]*media, error) {
	mediaList := []*media{}

	mediaList = append(mediaList, u.ProfilePicture())
	feedItems, err := u.Feed(insta, lastCheck)
	if err != nil {
		return nil, err
	}
	mediaList = append(mediaList, feedItems...)

	return mediaList, nil
}

func (u *User) ProfilePicture() *media {
	return &media{
		URL:      u.ProfilePictureURL,
		Path:     fmt.Sprintf("%s", Images),
		Username: u.Username,
	}
}

func (u *User) Items(insta *goinsta.Instagram, lastCheck string) (map[string]Item, error) {
	maxID := ""
	items := map[string]Item{}

	for {
		userFeedResponse, err := insta.UserFeed(u.ID, maxID, lastCheck)
		if err != nil {
			return nil, err
		}

		for _, item := range userFeedResponse.Items {
			items[item.ID] = Item(item)
		}

		if userFeedResponse.NextMaxID == "" {
			return items, err
		}
		maxID = userFeedResponse.NextMaxID
		time.Sleep(time.Duration(rand.Intn(3)) * time.Second)
	}
}

func (u *User) Feed(insta *MultiInsta, lastCheck string) ([]*media, error) {
	mediaList := []*media{}

	acc := insta.Main
	if !u.IsPrivate {
		acc = insta.Alternatives[rand.Intn(len(insta.Alternatives))]
	}

	fmt.Printf("Using %s to get %s Feed\n", acc.LoggedInUser.Username, u.Username)

	items, err := u.Items(acc, lastCheck)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		mediaList = append(mediaList, item.Images()...)
		mediaList = append(mediaList, item.Videos()...)
	}

	return mediaList, nil
}

func (i *Item) Images() []*media {
	mediaList := []*media{}

	for _, image := range i.ImageVersions2.Candidates {
		timePath := time.Unix(i.DeviceTimestamp, 0).Format("2006/01/02")

		mediaList = append(mediaList, &media{
			Username:  i.User.Username,
			Timestamp: i.DeviceTimestamp,
			URL:       image.URL,
			Path:      fmt.Sprintf("%s/%s/%d/%d", Images, timePath, image.Width, image.Height),
		})
	}

	return mediaList
}

func (i *Item) Videos() []*media {
	mediaList := []*media{}

	for _, video := range i.VideoVersions {
		timePath := time.Unix(i.DeviceTimestamp, 0).Format("2006/01/02")

		mediaList = append(mediaList, &media{
			Username:  i.User.Username,
			Timestamp: i.DeviceTimestamp,
			URL:       video.URL,
			Path:      fmt.Sprintf("%s/%s/%d/%d/%d", Videos, timePath, video.Width, video.Height, video.Type),
		})
	}

	return mediaList
}

func NewInsta(username, password string) (*goinsta.Instagram, error) {
	insta := goinsta.New(username, password)

	if err := insta.Login(); err != nil {
		return nil, err
	}

	return insta, nil
}

func main() {
	mainUsername := ""
	mainPassword := ""
	altUsername := ""
	altPassword := ""
	numDownloaders := 5
	maxWaitTime := 60

	mainInsta, err := NewInsta(mainUsername, mainPassword)
	if err != nil {
		panic(err)
	}
	altInsta, err := NewInsta(altUsername, altPassword)
	if err != nil {
		panic(err)
	}

	insta := &MultiInsta{
		Main: mainInsta,
		Alternatives: []*goinsta.Instagram{
			altInsta,
		},
	}

	mediaChannel := make(chan *media, 100000)

	go func() {
		for {
			goterm.MoveCursor(goterm.Width()-20, goterm.Height()-1)
			goterm.Printf("Queue: %d", len(mediaChannel))

			goterm.MoveCursor(1, goterm.Height()-1)
			goterm.Flush()

			time.Sleep(50 * time.Millisecond)
		}
	}()

	//Start the download goroutine
	for i := 0; i < numDownloaders; i++ {
		go func() {
			for m := range mediaChannel {
				err := download(m, maxWaitTime)
				if err != nil {
					panic(err)
				}
			}
		}()
	}

	lastCheck := map[int64]time.Time{}

	for {
		fmt.Println("Checking new stories")
		tray, err := insta.Main.GetReelsTrayFeed()
		if err != nil {
			panic(err)
		}

		trayResponse := Tray(tray)

		for _, m := range trayResponse.Media() {
			mediaChannel <- m
		}

		userList, err := users(insta.Main)
		if err != nil {
			panic(err)
		}

		for _, user := range userList {
			fmt.Printf("Checking %s\n", user.Username)
			lastTimestamp := ""

			if last, ok := lastCheck[user.ID]; ok {
				lastTimestamp = fmt.Sprintf("%d", last.Unix())
			}

			userMedia, err := user.Media(insta, lastTimestamp)
			if err != nil {
				panic(err)
			}
			lastCheck[user.ID] = time.Now()

			for _, m := range userMedia {
				mediaChannel <- m
			}

			time.Sleep(1 * time.Second)
		}

		time.Sleep(5 * time.Minute)
	}

	if err := insta.Main.Logout(); err != nil {
		panic(err)
	}

	for _, alt := range insta.Alternatives {
		if err := alt.Logout(); err != nil {
			panic(err)
		}
	}
}

func users(insta *goinsta.Instagram) (map[int64]User, error) {
	maxID := ""
	users := map[int64]User{}

	for {
		userResp, err := insta.UserFollowing(insta.LoggedInUser.ID, maxID)
		if err != nil {
			return nil, err
		}

		for _, user := range userResp.Users {
			users[user.ID] = User(user)
		}

		if userResp.NextMaxID == "" {
			return users, err
		}
		maxID = userResp.NextMaxID
		time.Sleep(time.Duration(rand.Intn(3)) * time.Second)
	}
}

func download(media *media, maxWaitTime int) error {
	path := fmt.Sprintf("%s/%s", media.Username, media.Path)
	os.MkdirAll(path, os.ModePerm)

	u, err := url.Parse(media.URL)
	if err != nil {
		return err
	}

	urlParts := strings.Split(u.Path, "/")

	filename := urlParts[len(urlParts)-1]
	tm := time.Unix(media.Timestamp, 0)

	filePath := fmt.Sprintf("%s/%s_%s", path, tm.Format("20060102_150405"), filename)

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		fmt.Printf("Skipping: %s \n", filePath)
		return nil
	}

	out, err := os.Create(filePath)
	if err != nil {
		return err
	}

	fmt.Printf("Downloading: %s \n", filePath)

	time.Sleep(time.Duration(rand.Intn(maxWaitTime)) * time.Second)
	resp, err := http.Get(media.URL)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	if err = resp.Body.Close(); err != nil {
		return err
	}

	if err = out.Close(); err != nil {
		return err
	}

	return nil
}
