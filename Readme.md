# livedl
新配信(HTML5)に対応したニコ生録画ツール。ニコ生以外のサイトにも対応予定

## 使い方
https://himananiito.hatenablog.jp/entry/livedl
を参照


## Linux(Ubuntu)でのビルド方法
```
cat /etc/os-release
NAME="Ubuntu"
VERSION="16.04.2 LTS (Xenial Xerus)"
```

### Go実行環境のインストール　（無い場合）
```
wget https://dl.google.com/go/go1.10.3.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.10.3.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
# 必要であれば、bashrcなどにPATHを追加する
```

### gitをインストール　（無い場合）
```
sudo apt-get install git
```

### gccなどのビルドツールをインストール　（無い場合）
```
sudo apt-get install build-essential
```

### 必要なgoのモジュールをインストール
```
go get github.com/gorilla/websocket
go get golang.org/x/crypto/sha3
go get github.com/mattn/go-sqlite3
go get github.com/gin-gonic/gin
```

### livedlのソースを取得
```
git clone https://github.com/himananiito/livedl.git
```

### livedlのコンパイル

ディレクトリを移動
```
cd livedl
```

#### (オプション)特定のバージョンを選択する場合
```
$ git tag
20180513.6
20180514.7
...
20180729.21
20180807.22
$ git checkout 20180729.21 （選んだバージョン）
```

#### (オプション)最新のコードをビルドする場合
```
git checkout master
```

ビルドする
```
go build src/livedl.go
```
もし、cannot find package "github.com/gin-gonic/gin" in any of:

など出る場合は、
`go get github.com/gin-gonic/gin` (適宜読み替える)したのち`go build src/livedl.go`を再実行する

```
./livedl -h
livedl (20180807.22-linux)
```

## Windows(32bit及び64bit上での32bit向け)コンパイル方法

### gccのインストール

gcc には必ず以下を使用すること。

http://tdm-gcc.tdragon.net/download

環境変数で（例）`C:\TDM-GCC-64\bin`が他のgccより優先されるように設定すること。

### 必要なgoのモジュール

linuxの説明に倣ってインストールする。

### コンパイル

PowerSellで、`build-386.ps1` を実行する。または以下を実行する。

```
set-item env:GOARCH -value 386
set-item env:CGO_ENABLED -value 1
go build -o livedl.x86.exe src/livedl.go
```

## 32bit環境で`x509: certificate signed by unknown authority`が出る

動けばいいのであればオプションで以下を指定する。

`-http-skip-verify=on`

以上