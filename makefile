.DEFAULT_GOAL := build
.PHONY : build

IMAGE_NAME:=wechat-hub

build:
	docker build -f build/Dockerfile -t ${IMAGE_NAME} .
