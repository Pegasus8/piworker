## List of APIs
All the APIs have the base url `http(s)://<host>/api/`
Some of these APIs need authentication, so each petition needs have a header (`Token`) containing the given token.

### Login
- [x] Path: **`/api/login`**. 
      Method: **POST**. 
      POST data: **`{"user": "<username>", "password": "<user_password>"}`**.
      Retreived data: **`{"successful": true/false, "token": "<user_token>", "expiresAt": <expiration_date(unix)>}`**.

### Tasks management
- [x] Path: **`/api/tasks/new`**. 
      Method: **POST**.
      Body: **[UserTask](https://github.com/Pegasus8/PiWorker/blob/6b6f13a04a2d23b782be2c6918a52490e71129a8/core/data/dataModel.go#L9)**.
      Retreived data: **`{"successful": true/false, "error": ""}`**. 
      **Token required**
- [x] Path: **`/api/tasks/modify`**. 
      Method: **POST**. 
      Body: **[UserTask](https://github.com/Pegasus8/PiWorker/blob/6b6f13a04a2d23b782be2c6918a52490e71129a8/core/data/dataModel.go#L9)**.
      Retreived data: **`{"successful": true/false, "error": ""}`**. 
      **Token required**
      *Warning: this API uses the taskname (`UserTask.TaskInfo.Name`) to find the task to modify. Once finded, the task will be overwritten. **Be careful using it outside of the WebUI.***
- [x] Path: **`/api/tasks/delete`**. 
      Method: **POST**.
      Body: **`{"taskname": ""}`**.
      Retreived data: **`{"successful": true/false, "error": ""}`**. 
      **Token required**
- [x] Path: **`/api/tasks/get-all`**. 
      Method: **GET**. 
      Body: **`{}`** (not required)
      Retreived data: **[[UserData.Tasks](https://github.com/Pegasus8/PiWorker/blob/6b6f13a04a2d23b782be2c6918a52490e71129a8/core/data/dataModel.go#L4)]**. 
      **Token required**

### Logs
- [ ] Path: **`/api/tasks/logs`**.
      Method: **GET**.
      Body: **`{"taskname": "", "from": "dd/mm/yyyy", "to": "dd/mm/yyyy"}`**. *Leave `taskname` field empty to get all the logs.*
      Retreived data: **`{"logs": ["one line", "another line", "..."]}`**

### Statistics (**Not implemented**)
- [ ] Path: **`/api/info/statistics`**. 
      Method: **GET**. 
      Retreived data: **`{}`**. 
     **Token required**

______
## WebSocket
- [x] Path: **`/ws`**. 
      **Token required**
_____
