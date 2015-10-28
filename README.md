# About

Yet another live reload app for golang, it watch xxx.go files only.

# Install

go get github.com/ot24net/umon
cd $GOPATH/src/github.com/ot24net/umon
go build
sudo cp umon /usr/local/bin

# Usage

```
umon dir1 dir2 dir3 ...
```

# Example

```
#!/bin/bash

# cd to main.go dir
cd app

# WEBAPP is environment var for app
WEBAPP=$HOME/work/webapp umon ../app ../lib ../model ../ctrl

```
  
  
