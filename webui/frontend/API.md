# List of APIs
All the APIs have the base url `http(s)://<host>/api/`
Some of these APIs need authentication, so at the end of each url the parameter `?token=<TOKEN_ID>` must be added, where `TOKEN_ID` is the logged user's token.
For login on the WebUI, the API needs an aditional parameter that is the `MASTER_KEY`. What is the `MASTER_KEY`? Is the key generated at the moment of installation. Without this, the user can't log in and because of that, it will never get the token for authenticate the APIs with authentication.

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

## Exclusive WebUI usage - **Needs the master key**
### Login
* Method: POST
* Url: `/auth?key=<MASTER_KEY>`
* Need auth: **yes**
* Need master key: **yes(!!)**



