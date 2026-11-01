package vk

import (
	"fmt"
	"strconv"

	vkapi "github.com/SevereCloud/vksdk/v3/api"
)

// EditPost updates text while retaining the photos of posts published by OneVoice.
// Unsupported attachments fail closed rather than silently stripping media.
func (c *Client) EditPost(groupID string, postID int, text string) error {
	if err := c.wait(); err != nil {
		return fmt.Errorf("vk edit post: wait: %w", err)
	}
	existing, err := c.vk.WallGetByID(vkapi.Params{"posts": groupID + "_" + strconv.Itoa(postID)})
	if err != nil {
		return fmt.Errorf("vk wall.getById: %w", err)
	}
	if len(existing.Items) != 1 || existing.Items[0].ID != postID || strconv.Itoa(existing.Items[0].OwnerID) != groupID {
		return fmt.Errorf("vk: post not found in connected community")
	}
	attachments := make([]string, 0, len(existing.Items[0].Attachments))
	for _, attachment := range existing.Items[0].Attachments {
		if attachment.Type != "photo" {
			return fmt.Errorf("vk: editing attachment type %q is unsupported", attachment.Type)
		}
		ref := attachment.Photo.ToAttachment()
		if attachment.Photo.AccessKey != "" {
			ref += "_" + attachment.Photo.AccessKey
		}
		attachments = append(attachments, ref)
	}
	if err := c.wait(); err != nil {
		return fmt.Errorf("vk edit post: wait: %w", err)
	}
	_, err = c.vk.WallEdit(vkapi.Params{"owner_id": groupID, "post_id": postID, "message": text, "attachments": attachments})
	if err != nil {
		return fmt.Errorf("vk wall.edit: %w", err)
	}
	return nil
}

// DeletePost deletes a post from the explicitly selected community wall.
func (c *Client) DeletePost(groupID string, postID int) error {
	if err := c.wait(); err != nil {
		return fmt.Errorf("vk delete post: wait: %w", err)
	}
	deleted, err := c.vk.WallDelete(vkapi.Params{"owner_id": groupID, "post_id": postID})
	if err != nil {
		return fmt.Errorf("vk wall.delete: %w", err)
	}
	if deleted != 1 {
		return fmt.Errorf("vk wall.delete: deletion was not confirmed")
	}
	return nil
}
