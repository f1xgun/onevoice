package vk

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	vkapi "github.com/SevereCloud/vksdk/v3/api"
	"golang.org/x/time/rate"
)

// Client wraps the VK API for the OneVoice VK agent.
type Client struct {
	vk      *vkapi.VK
	limiter *rate.Limiter
}

// New creates a new Client with the given access token.
// A rate limiter of 3 requests/sec (burst 1) is applied to all VK API calls.
func New(accessToken string) *Client {
	return &Client{
		vk:      vkapi.NewVK(accessToken),
		limiter: rate.NewLimiter(3, 1),
	}
}

// NewWithBaseURL creates a Client pointing at a custom API base URL (for testing).
// The baseURL should end with a slash, e.g. "http://localhost:1234/method/".
func NewWithBaseURL(accessToken, baseURL string) *Client {
	vk := vkapi.NewVK(accessToken)
	vk.MethodURL = baseURL
	return &Client{
		vk:      vk,
		limiter: rate.NewLimiter(rate.Inf, 1), // no rate limit in tests
	}
}

// wait blocks until the rate limiter allows the next request.
func (c *Client) wait() error {
	return c.limiter.Wait(context.Background())
}

// PublishPost publishes a post to a VK community wall.
// groupID should be the owner_id (negative for communities, e.g. "-123456").
func (c *Client) PublishPost(groupID, text string) (int64, error) {
	if err := c.wait(); err != nil {
		return 0, err
	}
	resp, err := c.vk.WallPost(vkapi.Params{
		"owner_id": groupID,
		"message":  text,
	})
	if err != nil {
		return 0, fmt.Errorf("vk wall.post: %w", err)
	}
	return int64(resp.PostID), nil
}

// PostPhoto downloads an image from photoURL, uploads it to VK via UploadGroupWallPhoto,
// then publishes a wall post with the photo attachment and optional caption.
func (c *Client) PostPhoto(groupID, photoURL, caption string) (int64, error) {
	if err := c.wait(); err != nil {
		return 0, err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get(photoURL)
	if err != nil {
		return 0, fmt.Errorf("vk: download image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("vk: download image: status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		return 0, fmt.Errorf("vk: unexpected content-type %q, expected image/*", ct)
	}

	numericID := strings.TrimPrefix(groupID, "-")
	groupIDInt, err := strconv.Atoi(numericID)
	if err != nil {
		return 0, fmt.Errorf("vk: invalid group_id %q: %w", groupID, err)
	}

	photos, err := c.vk.UploadGroupWallPhoto(groupIDInt, resp.Body)
	if err != nil {
		return 0, fmt.Errorf("vk photos.saveWallPhoto: %w", err)
	}
	if len(photos) == 0 {
		return 0, fmt.Errorf("vk: no photos returned from VK")
	}

	attachment := fmt.Sprintf("photo%d_%d", photos[0].OwnerID, photos[0].ID)
	wallResp, err := c.vk.WallPost(vkapi.Params{
		"owner_id":    "-" + strconv.Itoa(groupIDInt),
		"message":     caption,
		"attachments": attachment,
	})
	if err != nil {
		return 0, fmt.Errorf("vk wall.post (photo): %w", err)
	}
	return int64(wallResp.PostID), nil
}

// SchedulePost publishes a postponed post to a VK community wall.
// VK holds the post and automatically publishes it at publishDate (Unix timestamp).
func (c *Client) SchedulePost(groupID, text string, publishDate int64) (int64, error) {
	if err := c.wait(); err != nil {
		return 0, err
	}
	resp, err := c.vk.WallPost(vkapi.Params{
		"owner_id":     groupID,
		"message":      text,
		"publish_date": publishDate,
	})
	if err != nil {
		return 0, fmt.Errorf("vk wall.post (scheduled): %w", err)
	}
	return int64(resp.PostID), nil
}

// UpdateGroupInfo updates the description of a VK community.
// groupID may be negative (e.g. "-123456") or bare numeric string ("123456").
func (c *Client) UpdateGroupInfo(groupID, description string) error {
	if err := c.wait(); err != nil {
		return err
	}
	numericID := strings.TrimPrefix(groupID, "-")
	id, err := strconv.ParseInt(numericID, 10, 64)
	if err != nil {
		return fmt.Errorf("vk: invalid group_id %q: %w", groupID, err)
	}

	_, err = c.vk.GroupsEdit(vkapi.Params{
		"group_id":    id,
		"description": description,
	})
	if err != nil {
		return fmt.Errorf("vk groups.edit: %w", err)
	}
	return nil
}

// GetComments retrieves comments from a specific VK wall post.
// groupID should be the owner_id (negative for communities, e.g. "-123456").
// postID is required by VK API.
func (c *Client) GetComments(groupID string, postID, count int) ([]map[string]interface{}, error) {
	if err := c.wait(); err != nil {
		return nil, err
	}

	resp, err := c.vk.WallGetComments(vkapi.Params{
		"owner_id": groupID,
		"post_id":  postID,
		"count":    count,
		"extended": 0,
	})
	if err != nil {
		return nil, fmt.Errorf("vk wall.getComments: %w", err)
	}

	comments := make([]map[string]interface{}, 0, len(resp.Items))
	for _, item := range resp.Items {
		comments = append(comments, map[string]interface{}{
			"id":      item.ID,
			"text":    item.Text,
			"date":    item.Date,
			"from_id": item.FromID,
			"post_id": postID,
		})
	}
	return comments, nil
}

// ReplyComment creates a threaded reply to a wall comment.
// groupID should be the owner_id (negative for communities, e.g. "-123456").
func (c *Client) ReplyComment(groupID string, postID, commentID int, text string) (int, error) {
	if err := c.wait(); err != nil {
		return 0, err
	}
	resp, err := c.vk.WallCreateComment(vkapi.Params{
		"owner_id":         groupID,
		"post_id":          postID,
		"message":          text,
		"reply_to_comment": commentID,
	})
	if err != nil {
		return 0, fmt.Errorf("vk wall.createComment: %w", err)
	}
	return resp.CommentID, nil
}

// DeleteComment removes a comment from a wall post.
// groupID should be the owner_id (negative for communities, e.g. "-123456").
func (c *Client) DeleteComment(groupID string, commentID int) error {
	if err := c.wait(); err != nil {
		return err
	}
	_, err := c.vk.WallDeleteComment(vkapi.Params{
		"owner_id":   groupID,
		"comment_id": commentID,
	})
	if err != nil {
		return fmt.Errorf("vk wall.deleteComment: %w", err)
	}
	return nil
}

// GetCommunityInfo fetches community metadata: name, description, members count, links.
// groupID may be negative (e.g. "-123456") or bare numeric string ("123456").
func (c *Client) GetCommunityInfo(groupID string) (map[string]interface{}, error) {
	if err := c.wait(); err != nil {
		return nil, err
	}
	numericID := strings.TrimPrefix(groupID, "-")

	resp, err := c.vk.GroupsGetByID(vkapi.Params{
		"group_id": numericID,
		"fields":   "description,members_count,status,site,links,counters",
	})
	if err != nil {
		return nil, fmt.Errorf("vk groups.getById: %w", err)
	}
	if len(resp.Groups) == 0 {
		return nil, fmt.Errorf("vk: community not found for group_id %s", groupID)
	}

	g := resp.Groups[0]
	info := map[string]interface{}{
		"name":          g.Name,
		"screen_name":   g.ScreenName,
		"description":   g.Description,
		"members_count": g.MembersCount,
		"status":        g.Status,
		"site":          g.Site,
		"photo":         g.Photo200,
	}

	if len(g.Links) > 0 {
		links := make([]map[string]interface{}, 0, len(g.Links))
		for _, l := range g.Links {
			links = append(links, map[string]interface{}{
				"id":   l.ID,
				"url":  l.URL,
				"name": l.Name,
			})
		}
		info["links"] = links
	}

	return info, nil
}

// GetWallPosts fetches recent wall posts with engagement stats.
// groupID should be the owner_id (negative for communities, e.g. "-123456").
func (c *Client) GetWallPosts(groupID string, count int) (posts []map[string]interface{}, total int, err error) {
	if waitErr := c.wait(); waitErr != nil {
		return nil, 0, waitErr
	}
	resp, err := c.vk.WallGet(vkapi.Params{
		"owner_id": groupID,
		"count":    count,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("vk wall.get: %w", err)
	}

	posts = make([]map[string]interface{}, 0, len(resp.Items))
	for _, item := range resp.Items {
		post := map[string]interface{}{
			"id":       item.ID,
			"text":     item.Text,
			"date":     item.Date,
			"likes":    item.Likes.Count,
			"comments": item.Comments.Count,
			"reposts":  item.Reposts.Count,
			"views":    item.Views.Count,
		}
		posts = append(posts, post)
	}
	return posts, resp.Count, nil
}
