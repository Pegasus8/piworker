<template>
  <div class="m-4 bg-dark card">
    <div class="card-header h4 font-weight-bold text-light">Sign in</div>
    <div class="card-body container">
      <div class="form-group text-left">
        <label for="username" class="text-muted">Username</label>
        <input
          type="text"
          class="form-control"
          id="username"
          aria-describedby="usernamelHelp"
          placeholder="Enter your username"
          v-model="username"
        />
      </div>
      <div class="form-group text-left">
        <label for="password" class="text-muted">Password</label>
        <input
          type="password"
          class="form-control"
          id="exampleInputPassword1"
          placeholder="Password"
          v-model="password"
        />
      </div>
      <div class="form-group form-check">
        <input type="checkbox" class="form-check-input" id="keepLoggedCheckbox" />
        <label class="form-check-label text-light" for="keepLoggedCheckbox">Keep me logged</label>
      </div>
      <button 
        class="btn btn-primary btn-lg"
        @click="login"
        >
        Login
        </button>
    </div>
  </div>
</template>

<script>
import axios from "axios";
export default {
  data() {
    return {
      username: '',
      password: ''
    }
  },
  methods: {
    login() {
      if (!this.username || !this.password){
        return
      }
      console.log("post data:", this.username, this.password)
      axios.post("/api/login", {
          'username': this.username,
          'password': this.password
        })
        .then(response => {})
        .catch(err => {
          console.error(err);
        })
    }
  }
};
</script>

<style lang="scss" scoped>
</style>