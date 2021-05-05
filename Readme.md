# livedl
新配信(HTML5)に対応したニコ生録画ツール。ニコ生以外のサイトにも対応予定

## 使い方
https://himananiito.hatenablog.jp/entry/livedl
を参照

## Windowsでのビルド(exeを作成するためにDockerを利用)
### Step1
`docker-compose` が実行できるようにDocker Desktop for Windowsをインストールする。

### Step2
ターミナルで
```
build\windows
```
に移動する。

### Step3
```
docker-compose up --build
```
を実行するとプロジェクトのトップディレクトリに `livedl.exe` が作成される。

## Linux(Ubuntu)でのビルド方法
```
cat /etc/os-release
NAME="Ubuntu"
VERSION="16.04.2 LTS (Xenial Xerus)"
```

### Go実行環境のインストール　（無い場合）
```
https://golang.org/doc/install
に従う
```

### gitをインストール　（無い場合）
```
sudo apt-get install git
```

### gccなどのビルドツールをインストール　（無い場合）
```
sudo apt-get install build-essential
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

#### (オプション)最新のコードをビルドする場合
```
git checkout master
```

ビルドする
```
go build src/livedl.go
```

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

## コンテナで実行

### livedlのソースを取得
```
git clone https://github.com/himananiito/livedl.git
cd livedl
git checkout master # Or another version that supports docker (contains Dockerfile)
```

### イメージ作成
```
docker build -t livedl .
```

### イメージの使い方

- 出力フォルダを/livedlにマウント

```
docker run --rm -it -v "$(pwd):/livedl" livedl "https://live.nicovideo.jp/watch/..."
```

以上
