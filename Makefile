.PHONY: build install

build:
	go build -o ocode .

install:
	go install .
