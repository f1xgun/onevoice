package vk

import (
	"fmt"
	"strconv"
	"strings"

	vkapi "github.com/SevereCloud/vksdk/v3/api"
)

// Client wraps the VK API for the OneVoice VK agent.
type Client struct {
	vk *vkapi.VK
}

// New creates a new Client with the given access token.
func New(accessToken string) *Client {
	return &Client{vk: vkapi.NewVK(accessToken)}
}

// PublishPost publishes a post to a VK community wall.
// groupID should be the owner_id (negative for communities, e.g. "-123456").
func (c *Client) PublishPost(groupID, text string) (int64, error) {
	resp, err := c.vk.WallPost(vkapi.Params{
		"owner_id": groupID,
		"message":  text,
	})
	if err != nil {
		return 0, fmt.Errorf("vk wall.post: %w", err)
	}
	return int64(resp.PostID), nil
}

// SchedulePost publishes a postponed post to a VK community wall.
// VK holds the post and automatically publishes it at publishDate (Unix timestamp).
func (c *Client) SchedulePost(groupID, text string, publishDate int64) (int64, error) {
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

// GetComments retrieves recent comments from a VK community wall.
// groupID should be the owner_id (negative for communities, e.g. "-123456").
func (c *Client) GetComments(groupID string, count int) ([]map[string]interface{}, error) {
	resp, err := c.vk.WallGetComments(vkapi.Params{
		"owner_id": groupID,
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
		})
	}
	return comments, nil
}
