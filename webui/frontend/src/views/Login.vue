<template>
<div class="container">
  <div class="m-4 bg-dark card">
    <div class="card-header h4 font-weight-bold text-light">Sign in</div>
    <div class="card-body">
      <div class="p-2 p-md-4">
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
        
      </div>
    </div>
    <div class="card-footer">
      <button 
        class="btn btn-primary btn-lg"
        @click="login"
        >
        Login
      </button>
    </div>
  </div>
</div>
</template>

<script>
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
      this.$store.dispatch('auth/login', {
        user: this.username,
        password: this.password
      })
    }
  }
}
</script>

<style lang="scss" scoped>
</style>