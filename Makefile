all: basedpaste

basedpaste: basedpaste.go go.mod go.sum
	go build -o $@

install: basedpaste
	mkdir -m755 -p ~/.local/bin ~/.local/share/basedpaste ~/.config/basedpaste
	install -m755 basedpaste ~/.local/bin/basedpaste
	install -m644 index.html ~/.local/share/basedpaste/index.html
	install -m644 config.toml.example ~/.config/basedpaste/config.toml

uninstall:
	rm -f ~/.local/bin/basedpaste
	rm -rf ~/.local/share/basedpaste
	rm -rf ~/.config/basedpaste

clean:
	rm -f basedpaste

.PHONY: all install clean
