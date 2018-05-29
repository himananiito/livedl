go run updatebuildno.go
go build src/livedl.go

$dir = "livedl"
$zip = "$dir.zip"
if(Test-Path -PathType Leaf $zip) {
	rm $zip
}
if(Test-Path -PathType Container $dir) {
	rmdir -Recurse $dir
}
mkdir $dir
cp livedl.exe $dir
cp Readme.md $dir

cp livedl-gui.exe $dir
cp livedl-gui.exe.config $dir
cp Newtonsoft.Json.dll $dir
cp Newtonsoft.Json.xml $dir

Compress-Archive -Path $dir -DestinationPath $zip

if(Test-Path -PathType Container $dir) {
	rmdir -Recurse $dir
}

