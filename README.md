# PiWorker 

***On development** / **En desarrollo***

*In the next days I will add objectives, documentation, characteristics, captures, etc; everything necessary to understand how it works (or how it will).*

*En los próximos días agregaré objetivos, documentación, características, capturas, etc; todo lo necesario para comprender cómo es el funcionamiento (o como lo será).*

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
10. Finally, compile the entire project, where `output_dir` is the path where the executable will be saved: `go build -o <output_dir>`

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
