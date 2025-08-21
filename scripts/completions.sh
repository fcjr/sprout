#!/bin/sh
set -e
rm -rf completions
mkdir completions
for sh in bash zsh fish; do
	go run cmd/sprout/main.go completion "$sh" >"completions/sprout.$sh"
done