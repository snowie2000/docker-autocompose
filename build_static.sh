#!/bin/bash

go build --ldflags '-s -w -linkmode external -extldflags "-static"'