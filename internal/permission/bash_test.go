package permission

import (
	"reflect"
	"testing"
)

func TestParseLineFindsEverySimpleCommand(t *testing.T) {
	for _, tc := range []struct {
		line  string
		want  [][]string
		risky bool
	}{
		{"go test ./...", [][]string{{"go", "test", "./..."}}, false},
		{"go vet ./... && go test ./... | tee /dev/null", [][]string{{"go", "vet", "./..."}, {"go", "test", "./..."}, {"tee", "/dev/null"}}, false},
		{"if true; then ls -la; fi", [][]string{{"true"}, {"ls", "-la"}}, false},
		{`'r'\m -rf "/tmp/x"`, [][]string{{"rm", "-rf", "/tmp/x"}}, false},
		{"go test ./... 2>&1 >/dev/null", [][]string{{"go", "test", "./..."}}, false},
		{"echo hi > out.txt", [][]string{{"echo", "hi"}}, true},
		{"echo hi >> out.txt", [][]string{{"echo", "hi"}}, true},
		{"cat <<EOF\nhi\nEOF", [][]string{{"cat"}}, false},
		{"GOFLAGS=-x go build", [][]string{{"go", "build"}}, true},
		{"echo $(rm -rf /)", [][]string{{"echo", ""}, {"rm", "-rf", "/"}}, true},
		{"diff <(ls a) b", [][]string{{"diff", "", "b"}, {"ls", "a"}}, true},
		{`find . -name x -exec rm {} \;`, [][]string{{"find", ".", "-name", "x", "-exec", "rm", "{}", ";"}}, true},
		{"go build -toolexec=/tmp/x ./...", [][]string{{"go", "build", "-toolexec=/tmp/x", "./..."}}, true},
		{"sudo ls", [][]string{{"sudo", "ls"}}, true},
		{"xargs rm < files", [][]string{{"xargs", "rm"}}, true},
		{"ls $HOME", [][]string{{"ls", ""}}, true},
		{"export X=1", nil, true},
		{"f() { rm -rf /; }", [][]string{{"rm", "-rf", "/"}}, true},
		{"echo 'unterminated", [][]string{{"echo", "'unterminated"}}, true},
		{"", nil, false},
	} {
		got := ParseLine(tc.line)
		if !reflect.DeepEqual(got.Commands, tc.want) || got.Risky != tc.risky {
			t.Errorf("ParseLine(%q) = %q risky=%v, want %q risky=%v", tc.line, got.Commands, got.Risky, tc.want, tc.risky)
		}
	}
}

func TestParseLineMarksFlagsThatRunProgramsRisky(t *testing.T) {
	for _, line := range []string{
		`go test -exec='sh -c id' ./...`,
		`go test -exec 'sh -c id' ./...`,
		`go test --exec=x ./...`,
		`go vet -vettool=/tmp/x ./...`,
		`go build -o /usr/local/bin/git .`,
		`rg --pre=./x foo`,
		`git grep -O'sh -c id' foo`,
		`git grep --open-files-in-pager=vi foo`,
		`sort --output=/home/u/.bashrc in`,
		`sort -o /home/u/.bashrc in`,
		`sort --compress-program=x in`,
		`tar -I 'sh -c id' -xf a.tar`,
		`tar -xIx -f a.tar`,
		`tar xIf x a.tar`,
		`tar --use-compress-program=x -xf a.tar`,
		`tar --to-command=x -xf a.tar`,
		`tar --checkpoint=1 --checkpoint-action=exec=x -cf a.tar .`,
		`git -c core.pager='sh -c id' log`,
		`git -c alias.st='!sh -c id' st`,
		`git --config-env=core.sshCommand=X fetch`,
		`git --exec-path=/tmp log`,
		`git clone -u 'sh -c id' repo`,
		`git clone --upload-pack=x repo`,
		`git rebase -x 'sh -c id' HEAD~1`,
		`git difftool --extcmd=x`,
		`find . -execdir id \;`,
		`find . -okdir id \;`,
		`find . -delete`,
		`find . -fprint /home/u/.bashrc`,
		`rsync -e 'sh -c id' a b:c`,
		`rsync -avze x a b:c`,
		`rsync --rsh=x a b:c`,
		`rsync --rsync-path=x a b:c`,
		`ssh -o ProxyCommand='sh -c id' host`,
		`ssh -oProxyCommand=x host`,
		`scp -S x a b:c`,
		`man -P 'sh -c id' ls`,
		`man --pager=x ls`,
		`less -o /home/u/.bashrc f`,
		`zip -TT x a.zip f`,
		`curl --output /home/u/.bashrc https://x`,
		`curl -o /home/u/.bashrc https://x`,
		`make --eval='x:;id' x`,
		`make test GO='sh -c id'`,
	} {
		if got := ParseLine(line); !got.Risky {
			t.Errorf("ParseLine(%q) is not risky", line)
		}
	}
	for _, line := range []string{
		`go test -cover -race -run 'TestX' ./...`,
		`go build ./...`,
		`git grep -n foo`,
		`git push -u origin main`,
		`git log --oneline -n 5`,
		`tar -cf a.tar dir`,
		`rsync -avz a b:c`,
		`ssh -v host uptime`,
		`sort -u in`,
		`find . -name '*.go' -type f`,
		`make test`,
	} {
		if got := ParseLine(line); got.Risky {
			t.Errorf("ParseLine(%q) is risky", line)
		}
	}
}

func TestParseLineMarksWordsBashExpandsRisky(t *testing.T) {
	for line, risky := range map[string]bool{
		`go test {-exec,'sh -c id'} ./...`: true,
		`go test ./... -{exec,x}=y`:        true,
		`{rm,-rf,x}`:                       true,
		`echo {1..3}`:                      true,
		`go test ./... *`:                  true,
		`go test ./... -e*`:                true,
		`go test ./... [-]exec`:            true,
		`go test ./... ''*`:                true,
		`r? -rf x`:                         true,
		`/bin/r[m] x`:                      true,
		`ls ./*.go src/*.go`:               false,
		`git reset HEAD@{1}`:               false,
		`echo '{a,b}' "*" \*`:              false,
		`[ -f go.mod ]`:                    false,
	} {
		if got := ParseLine(line); got.Risky != risky {
			t.Errorf("ParseLine(%q) risky = %v, want %v", line, got.Risky, risky)
		}
	}
}

func TestParseLineAllowsOnlyPlainSyntax(t *testing.T) {
	for line, risky := range map[string]bool{
		`go test ./... && [[ 'a[$(touch /tmp/pwn)]' -eq 0 ]]`: true,
		`[[ -v 'a[$(touch /tmp/pwn)]' ]]`:                     true,
		`[[ -f go.mod ]]`:                                     true,
		`(( a[$(id)] ))`:                                      true,
		`let 'a[$(id)]=1'`:                                    true,
		`time go test ./...`:                                  true,
		`case x in x) ls;; esac`:                              true,
		`for ((;;)); do ls; done`:                             true,
		`for f in a b; do echo $f; done`:                      true,
		`echo $"x"`:                                           true,
		`echo "$HOME"`:                                        true,
		`ls @(a|b)`:                                           true,
		`declare -a x`:                                        true,
		`(go test ./...)`:                                     false,
		`{ go vet ./...; go test ./...; }`:                    false,
		`if go vet ./...; then go test ./...; else ls; fi`:    false,
		`while false; do ls; done`:                            false,
		`for f in a b; do go test ./...; done`:                false,
		`! go test ./... |& tee /dev/null || ls`:              false,
		"go test ./... # run":                                 false,
		"cat <<'EOF'\n$(id)\nEOF":                             false,
	} {
		if got := ParseLine(line); got.Risky != risky {
			t.Errorf("ParseLine(%q) risky = %v, want %v", line, got.Risky, risky)
		}
	}
}
