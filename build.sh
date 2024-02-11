#! /bin/bash

clear

TRG_PKG='main'
BUILD_TIME=$(date +"%Y%m%d.%H%M%S")
CommitHash=N/A
GoVersion=N/A
GitTag=N/A

if [[ $(go version) =~ [0-9]+\.[0-9]+\.[0-9]+ ]];
then
    GoVersion=${BASH_REMATCH[0]}
fi

GV=$(git tag || echo 'N/A')
if [[ $GV =~ [^[:space:]]+ ]];
then
    GitTag=${BASH_REMATCH[0]}
fi

GH=$(git log -1 --pretty=format:%h || echo 'N/A')
if [[ GH =~ 'fatal' ]];
then
    CommitHash=N/A
else
    CommitHash=$GH
fi

FLAG="-X $TRG_PKG.BuildTime=$BUILD_TIME"
FLAG="$FLAG -X $TRG_PKG.CommitHash=$CommitHash"
FLAG="$FLAG -X $TRG_PKG.GoVersion=$GoVersion"
FLAG="$FLAG -X $TRG_PKG.GitTag=$GitTag"

if [[ $1 =~ '-i' ]];
then
    echo 'go install'
    echo go install -v -ldflags "$FLAG"
else
    echo 'go build'
    echo go build -v -ldflags "$FLAG"
fi

