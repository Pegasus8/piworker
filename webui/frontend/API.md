# List of APIs
All the APIs have the base url `http(s)://<host>/api/`
Some of these APIs need authentication, so at the end of each url the parameter `?token=<TOKEN_ID>` must be added, where `TOKEN_ID` is the logged user's token.

## User data APIs
### New task 
* Method: POST
* Url: `/tasks/new`
* Need auth: **yes**
* Data: 
  ```json
  //TODO 
  ```

### Modify an existent task (edit or delete)
* Method: POST
* Url: `/tasks/modify`
* Need auth: **yes**
* Data: 
  ```json
  //TODO 
  ```

### List of all the user's tasks 
* Method: GET
* Url: `/tasks/get-all`
* Need auth: **yes**
* Data: - 

## General PiWorker info
### General statistics
* Method: GET
* Url: `/info/statistics`
* Need auth: no
* Data: -




