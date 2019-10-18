<template>
<div class="container my-4">
  <div class="justify-content-center row">
    <div class="col-sm-9 col-md-7 col-lg-5 col-xl-4">
      <div class="bg-dark card">
        <div class="card-header h5 font-weight-bold text-light">Sign in</div>
        <div class="card-body">
          <form class>
            <div class="form-group text-left">
              <label for="username" class="text-light">Username</label>
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
              <label for="passwordInput" class="text-light">Password</label>
              <input
                type="password"
                class="form-control"
                id="passwordInput"
                placeholder="Password"
                v-model="password"
              />
            </div>
            
          </form>
        </div>
        <div class="card-footer">
          <button 
            class="btn btn-primary btn-lg btn-block"
            @click="login"
            >
            Login
          </button>
        </div>
      </div>
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