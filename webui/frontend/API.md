# List of APIs
All the APIs have the base url `http(s)://<host>/api/`
Some of these APIs need authentication, so at the end of each url the parameter `?token=<TOKEN_ID>` must be added, where `TOKEN_ID` is the logged user's token.

## User data APIs
### New task 
* Method: POST
* Url: `/tasks/new`
* Need token: **yes**
* Data: 
  ```json
  //TODO 
  ```

### Modify an existent task (edit or delete)
* Method: POST
* Url: `/tasks/modify`
* Need token: **yes**
* Data: 
  ```json
  //TODO 
  ```

### List of all the user's tasks 
* Method: GET
* Url: `/tasks/get-all`
* Need token: **yes**
* Data: - 

## General PiWorker info
### General statistics
* Method: GET
* Url: `/info/statistics`
* Need token: no
* Data: -

## Exclusive WebUI usage - **Needs user and password**
### Login
* Method: POST
* Url: `/auth`
* Need auth: **yes**(user and password)
* POST data: 
    ```json
    {
        user: "<username>",
        password: "<password>"
    }
    ```
* Response (in case of a valid user and password):
    ```json
    {
        authorized: true/false,
        data: {
            userID: "",
            token: "",
            expiresIn: 0
        }
    }
    ```



