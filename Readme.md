# livedl2 αバージョン

次期バージョンとなる予定のlivedl2の実験的機能評価用バージョンです。

## 使い方

`user-session.txt`にユーザーセッション情報（`user_session_XXXX_XXXXXX`）を書いて保存

livedl2の実行ファイルを起動し、
http://localhost:8080/
にアクセスして下さい。

### セッション情報の調べ方

**セッション情報は「絶対に」他人に流出しないようにして下さい**

例えばログイン済みのブラウザでニコ生トップページからデバッグコンソールを開き、以下のスクリプトを実行し、ブラウザ画面の文字列をコピーする。

```
var match = document.cookie.match(/user_session=(\w+)/); if(match) document.write(match[1]);
```

## ビルド方法

以下の`lived.go`を`livedl2.go`に読み換えて下さい。

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

### 32bit環境で`x509: certificate signed by unknown authority`が出る

動けばいいのであればオプションで以下を指定する。

`-http-skip-verify=on`

## Dockerでビルド

### livedlのソースを取得
```
git clone https://github.com/himananiito/livedl.git
cd livedl
git checkout master # Or another version that supports docker (contains Dockerfile)
```

### イメージ作成
```
docker build -t <your_image_tag> .
```

### イメージの使い方

- 出力フォルダを/livedlにマウント
- 通常のパラメーターに加えて`--no-chdir`を渡す

```
docker run -it --rm -v ~/livedl:/livedl <your_image_tag> livedl --no-chdir <other_parameters> ...
```

以上
