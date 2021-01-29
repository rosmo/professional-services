VERSION := 1.2.0

all: osx-x64 win-x64 win-x86 linux-x64

osx-x64:
	dotnet publish -r osx-x64 --self-contained=true -c Release -p:PublishSingleFile=true -p:PublishTrimmed=true
	mkdir -p bin/Release/netcoreapp5.0/osx-x64/publish/bin
	mkdir -p bin/Release/netcoreapp5.0/osx-x64/publish/lib
	mv bin/Release/netcoreapp5.0/osx-x64/publish/gsnapshot bin/Release/netcoreapp5.0/osx-x64/publish/bin
	mv bin/Release/netcoreapp5.0/osx-x64/publish/*.dylib bin/Release/netcoreapp5.0/osx-x64/publish/lib
	rm -f bin/Release/netcoreapp5.0/osx-x64/publish/*.pdb
	tar cfvz bin/gsnapshot-${VERSION}-osx-x64.tar.gz -C bin/Release/netcoreapp5.0/osx-x64/publish bin lib
  
win-x64:
	dotnet publish -r win-x64 --self-contained=true -c Release -p:PublishSingleFile=true -p:PublishTrimmed=true
	zip -j bin/gsnapshot-${VERSION}-win-x64.zip bin/Release/netcoreapp5.0/win-x64/publish/*
	wixl -v gsnapshot.wsx -o bin/gsnapshot-${VERSION}-x64.msi -D PublishDirectory=bin/Release/netcoreapp5.0/win-x64/publish/ -a x64 -D TargetPlatform=x64

win-x86:
	dotnet publish -r win-x86 --self-contained=true -c Release -p:PublishSingleFile=true -p:PublishTrimmed=true
	zip -j bin/gsnapshot-${VERSION}-win-x86.zip bin/Release/netcoreapp5.0/win-x86/publish/*
	wixl -v gsnapshot.wsx -o bin/gsnapshot-${VERSION}-x86.msi -D PublishDirectory=bin/Release/netcoreapp5.0/win-x86/publish/ -a x86 -D TargetPlatform=x86

linux-x64:
	dotnet publish -r linux-x64 --self-contained=true -c Release -p:PublishSingleFile=true -p:PublishTrimmed=true
	tar cfvz bin/gsnapshot-${VERSION}-linux-x64.tar.gz -C bin/Release/netcoreapp5.0/linux-x64/publish gsnapshot
