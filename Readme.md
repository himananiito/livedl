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

以上