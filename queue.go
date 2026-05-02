package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type QueueItem struct {
	ID       string    `json:"id"`
	From     string    `json:"from"`
	To       []string  `json:"to"`
	Data     []byte    `json:"data"`
	Created  time.Time `json:"created"`
	Attempts int       `json:"attempts"`
	NextTry  time.Time `json:"next_try"`
}

func enqueue(from string, to []string, data []byte) error {
	item := &QueueItem{
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		From:    from,
		To:      to,
		Data:    data,
		Created: time.Now(),
		NextTry: time.Now(),
	}
	log.Printf("[%s] queued from=%s to=%v size=%d bytes", item.ID, from, to, len(data))
	b, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfg.QueueDir, item.ID+".json"), b, 0600)
}

func loadQueue() []*QueueItem {
	entries, err := os.ReadDir(cfg.QueueDir)
	if err != nil {
		return nil
	}
	var items []*QueueItem
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(cfg.QueueDir, e.Name()))
		if err != nil {
			continue
		}
		var item QueueItem
		if err := json.Unmarshal(b, &item); err != nil {
			continue
		}
		items = append(items, &item)
	}
	return items
}

func updateItem(item *QueueItem) {
	b, _ := json.Marshal(item)
	os.WriteFile(filepath.Join(cfg.QueueDir, item.ID+".json"), b, 0600)
}

func deleteItem(item *QueueItem) {
	os.Remove(filepath.Join(cfg.QueueDir, item.ID+".json"))
}

func failItem(item *QueueItem) {
	b, _ := json.Marshal(item)
	os.WriteFile(filepath.Join(cfg.QueueDir, "failed", item.ID+".json"), b, 0600)
	deleteItem(item)
}

func startWorker() {
	for {
		time.Sleep(30 * time.Second)
		for _, item := range loadQueue() {
			if time.Now().Before(item.NextTry) {
				continue
			}
			processItem(item)
		}
	}
}

func processItem(item *QueueItem) {
	byDomain := make(map[string][]string)
	for _, to := range item.To {
		at := len(to) - 1
		for at >= 0 && to[at] != '@' {
			at--
		}
		if at > 0 {
			domain := to[at+1:]
			byDomain[domain] = append(byDomain[domain], to)
		}
	}

	allOk := true
	for domain, rcpts := range byDomain {
		if err := sendToDomain(item, domain, rcpts); err != nil {
			log.Printf("[%s] send to %s: %v", item.ID, domain, err)
			allOk = false
		}
	}

	if allOk {
		deleteItem(item)
		log.Printf("[%s] delivered", item.ID)
		return
	}

	item.Attempts++
	if item.Attempts >= cfg.RetryMax {
		log.Printf("[%s] max retries, dropping", item.ID)
		failItem(item)
		return
	}

	delay := time.Duration(1<<uint(item.Attempts)) * time.Minute
	if delay > 4*time.Hour {
		delay = 4 * time.Hour
	}
	item.NextTry = time.Now().Add(delay)
	updateItem(item)
}
