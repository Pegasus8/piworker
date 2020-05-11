# PiWorker 
-----
PiWorker is a **free** and **open source** tool that let you **automate tasks easily on your [Raspberry Pi](https://www.raspberrypi.org)** (can be used on other devices too) without letting aside your privacy. In these times where your data is used as a payment on some "free" software/services, I think is good to remark that PiWorker **does not** use any external server for nothing (unless you explicity add an action to doing it), so everything is executed inside your device, **under your control**.

I'm working hard to make PiWorker stable and robust, but for now, it's not even an alpha, so there should be bugs everywhere (If you see one, can you [let me know it](https://github.com/Pegasus8/piworker/issues/new/choose) please? that will be really helpful!) so don't use it on a device that contain something that you don't want to loose.

<br>

> _**Disclaimer**: I am not responsible for the misuse that may be given to this software, whether for legal purposes or not. Use it at your own risk._

-----
## Installation:
    
~`curl -sSL https://github.com/Pegasus8/PiWorker/raw/master/install.sh | sudo bash`~

## Installation from source:
1. Make sure you have `golang` installed and configured. If not, check [this](https://golang.org/doc/install).
2. Make sure you have `nodejs` and `npm` installed. If you don't, install them from [here](https://nodejs.org/en/) or [here](https://github.com/nodesource/distributions) (if you use linux maybe the last option will be more easy).
3. Check if you have `git` installed. If not (again), get it from [here](https://git-scm.com/downloads).
4. Install [pkger](https://github.com/markbates/pkger) in your `GOPATH` running `go get github.com/markbates/pkger/cmd/pkger`.
5. Download the source code: `git clone https://github.com/Pegasus8/PiWorker`.
6. Once downloaded, go inside the directory `cd PiWorker/`.
7. Go to the dir of the frontend `cd webui/frontend/`, install the dependencies `npm install`, and compile it `npm run build`.
8. Go back to the root PiWorker directory `cd ../..`.
9. Execute `pkger` to include the frontend inside the binary.
10. Compile the entire project (`output_dir` is the path where the executable will be saved): `go build -o <output_dir>`. In my case, I prefer save the executable on the directory `$HOME/PiWorker/`, so I will run the command `go build -o $HOME/PiWorker/`. *Note: the dir used must exist before the compiling.*
11. Go to the directory where you saved the executable: `cd <output_dir>`. In my case is `$HOME/PiWorker/`, so I execute: `cd $HOME/PiWorker/`.
12. Install the service of P.W. running the following command: `sudo ./piworker --service install`. *Why `sudo`?* Because you need `root` privileges to add a new service to the system. *The service **must** be installed?* No, isn't something essential. If you prefer don't install it, remember that P.W. won't be executed when you reboot the system.
13. **IMPORTANT** - Make a new user before start the service: `./piworker --new-user --username <your_username> --password <your_password> --admin`. Replace `<your_username>` with the username you will use and `<your_password>` with the password. Also, the `--admin` flag can be avoided if you don't want to give admin privileges to the user. *Note: you can add more users if you want.*
14. Optional (but recommended) - Generate a self-signed certificate for a secure connection (https) with the WebUI (**Warning**: **don't use the WebUI/REST APIs from outside the [LAN](https://en.wikipedia.org/wiki/Local_area_network)**. As a software in early development, can contain vulnerabilities that can be exploited by more experienced people with malicious intentions):
```bash
openssl req \
        -subj '/O=PiWorker' \
        -new \
        -newkey \
        rsa:2048 \
        -sha256 \
        -days 365 \
        -nodes \
        -x509 \
        -keyout server.key \
        -out server.crt
```
15. Start the service: `sudo ./piworker --service start`.


## Acknowledgments
### Frontend (JS - VueJS)
Dependency | License
--- | ---
[VueJS](https://vuejs.org/) | [![GitHub license](https://img.shields.io/github/license/vuejs/vue)](https://github.com/vuejs/vue/blob/dev/LICENSE)
[Vuetify](https://vuetifyjs.com) | [![GitHub license](https://img.shields.io/github/license/vuetifyjs/vuetify)](https://github.com/vuetifyjs/vuetify/blob/master/LICENSE.md)
[Vue-router](https://router.vuejs.org/) | [![GitHub license](https://img.shields.io/github/license/vuejs/vue-router)](https://github.com/vuejs/vue-router/blob/dev/LICENSE)
[Vuex](https://vuex.vuejs.org/) | [![GitHub license](https://img.shields.io/github/license/vuejs/vuex)](https://github.com/vuejs/vuex/blob/dev/LICENSE)
[Axios](https://github.com/axios/axios) | [![GitHub license](https://img.shields.io/github/license/axios/axios)](https://github.com/axios/axios/blob/master/LICENSE)
[Vue.Draggable](https://github.com/SortableJS/Vue.Draggable) | [![GitHub license](https://img.shields.io/github/license/SortableJS/Vue.Draggable)](https://github.com/SortableJS/Vue.Draggable/blob/master/LICENSE)
[Vue-uuid](https://github.com/VitorLuizC/vue-uuid) | [![GitHub license](https://img.shields.io/github/license/VitorLuizC/vue-uuid)](https://github.com/VitorLuizC/vue-uuid/blob/master/LICENSE)
[Anime.js](https://animejs.com/) | [![GitHub license](https://img.shields.io/github/license/juliangarnier/anime)](https://github.com/juliangarnier/anime/blob/master/LICENSE.md)
[Chart.js](https://www.chartjs.org/) | [![GitHub license](https://img.shields.io/github/license/chartjs/Chart.js)](https://github.com/chartjs/Chart.js/blob/master/LICENSE.md)
[Vue-chartjs](https://vue-chartjs.org) | [![GitHub license](https://img.shields.io/github/license/apertureless/vue-chartjs)](https://github.com/apertureless/vue-chartjs/blob/develop/LICENSE.txt)
[typeface-roboto (Google Roboto)](https://github.com/KyleAMathews/typefaces/tree/master/packages/roboto) | -
[Material Design Icons](https://materialdesignicons.com/) | -

### Backend (Go)
Dependency | License
--- | ---
[Websocket by Gorilla](https://github.com/gorilla/websocket) | [![GitHub license](https://img.shields.io/github/license/gorilla/websocket)](https://github.com/gorilla/websocket/blob/master/LICENSE)
[Mux](https://github.com/gorilla/mux) | [![GitHub license](https://img.shields.io/github/license/gorilla/mux)](https://github.com/gorilla/mux/blob/master/LICENSE)
[Go-sqlite3](https://github.com/mattn/go-sqlite3) | [![GitHub license](https://img.shields.io/github/license/mattn/go-sqlite3)](https://github.com/mattn/go-sqlite3/blob/master/LICENSE)
[Pkger](https://github.com/markbates/pkger) | [![GitHub license](https://img.shields.io/github/license/markbates/pkger)](https://github.com/markbates/pkger/blob/master/LICENSE)
[Jwt-go](https://github.com/dgrijalva/jwt-go) | [![GitHub license](https://img.shields.io/github/license/dgrijalva/jwt-go)](https://github.com/dgrijalva/jwt-go/blob/master/LICENSE)
[Lumberjack](https://github.com/natefinch/lumberjack) | [![GitHub license](https://img.shields.io/github/license/natefinch/lumberjack)](https://github.com/natefinch/lumberjack/blob/v2.0/LICENSE)
[UUID by Google](https://github.com/google/uuid) | [![GitHub license](https://img.shields.io/github/license/google/uuid)](https://github.com/google/uuid/blob/master/LICENSE)
[Service](https://github.com/kardianos/service) | [![GitHub license](https://img.shields.io/github/license/kardianos/service)](https://github.com/kardianos/service/blob/master/LICENSE)
[Zerolog](https://github.com/rs/zerolog) | [![GitHub license](https://img.shields.io/github/license/rs/zerolog)](https://github.com/rs/zerolog/blob/master/LICENSE)
[Gopsutil](https://github.com/shirou/gopsutil) | [![GitHub license](https://img.shields.io/badge/license-BSD-green)](https://github.com/shirou/gopsutil/blob/master/LICENSE)
