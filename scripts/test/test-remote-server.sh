#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
cd $DIR

re="^(([^@]+)@)?([-_.A-Za-z0-9]+)(:([0-9]+))?$"
if [[ ! ( "$1" =~ $re ) ]]; then
  echo "usage: $0 user@host:port listen-port"
  exit 1
fi

ssh_user="${BASH_REMATCH[2]:-${USER}}"
ssh_host="${BASH_REMATCH[3]}"
ssh_port="${BASH_REMATCH[5]:-22}"
echoping_listen_port="${2:-12345}"

trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

echo "stop old echoping process on remote server ..."
ssh -p "$ssh_port" "$ssh_user"@"$ssh_host" "pkill -f /tmp/echoping"

echo "build echoping ..."
proj_base_dir="$DIR/../.."
proj_temp_dir="$DIR/../../temp"
mkdir -p "$proj_temp_dir"

if ! GOOS=linux GOARCH=amd64 go build -o "$proj_temp_dir/echoping.linux" "$proj_base_dir/cmd/echoping" ; then
  echo "go build linux failed"
  exit 1
fi

if ! go build -o "$proj_temp_dir/echoping" "$proj_base_dir/cmd/echoping" ; then
  echo "go build local os failed"
  exit 1
fi

echo "copy echoping to remote server ..."
scp -P "$ssh_port" "$proj_temp_dir/echoping.linux" "$ssh_user"@"$ssh_host":/tmp/echoping
ssh -p "$ssh_port" "$ssh_user"@"$ssh_host" "chmod a+x /tmp/echoping"

echo "run echoping server to listen $echoping_listen_port port on remote server ..."
ssh -p "$ssh_port" "$ssh_user"@"$ssh_host" "nohup /tmp/echoping -listen :$echoping_listen_port > /tmp/echoping.out 2>&1 &"

echo "watch echoping server's output ..."
ssh -p "$ssh_port" "$ssh_user"@"$ssh_host" "tail -n +0 -f /tmp/echoping.out" &

echo "run echoping client on local ..."
"$proj_temp_dir/echoping" -connect "$ssh_host:$echoping_listen_port"
