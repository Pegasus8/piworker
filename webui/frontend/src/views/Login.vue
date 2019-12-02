<template>
<b-container class="my-4">
  <b-row class="justify-content-center">
    <b-col sm="9" md="7" lg="5" xl="4">
      <b-card bg-variant="dark" text-variant="light">
        <template v-slot:header>
          <h5 class="font-weight-bold">Sign in</h5>
        </template>
        <b-form>
          <b-form-group
            class="text-left"
            label="Username"
            label-for="username"
          >
            <b-form-input
              id="username"
              type="text"
              placeholder="Enter your username"
              v-model="form.username"
              aria-describedby="usernamelHelp"
              required
            />
          </b-form-group>
          <b-form-group
            class="text-left"
            label="Password"
            label-for="passwordInput"
          >
            <b-form-input
              id="passwordInput"
              type="password"
              placeholder="Enter your password"
              aria-describedby="passwordHelp"
              v-model="form.password"
              required
            />
          </b-form-group>
        </b-form>
        <template v-slot:footer>
          <b-button variant="primary" size="lg" @click="login" block>
            Login
          </b-button>
        </template>
      </b-card>
      <transition name="fade">
        <div
          v-if="showAlert"
          class="alert alert-warning mt-3"
          role="alert"
        >
          Wrong user/password
        </div>
      </transition>
      <transition name="fade">
        <div
          v-if="errorOnLogin"
          class="alert alert-danger mt-3"
          role="alert"
        >
          <h4>Error when trying to login</h4>
          <p>Please, check your connection with PiWorker.</p>
          <hr>
          <p>Error: {{ error }}</p>
        </div>
      </transition>
    </b-col>
  </b-row>
</b-container>
</template>

<script>
export default {
  data () {
    return {
      form: {
        username: '',
        password: ''
      },
      waintingResponse: false,
      showAlert: false,
      errorOnLogin: false,
      error: ''
    }
  },
  methods: {
    login (event) {
      if (this.waintingResponse) {
        return // Prevent multiple requests
      }
      if (!this.form.username || !this.form.password) {
        return
      }
      event.preventDefault()
      this.waintingResponse = true
      this.$store.dispatch('auth/login', {
        user: this.form.username,
        password: this.form.password
      })
        .then((response) => {
          this.waintingResponse = false
          if (!response.successful) {
            this.showAlert = true
            setTimeout(() => this.showAlert = false, 3000)
          }
        })
        .catch((err) => {
          this.waintingResponse = false
          this.errorOnLogin = true
          this.error = err.message
          console.error('Error when trying to login:', err)
          setTimeout(() => {
            this.errorOnLogin = false
            this.error = ''
          }, 15000)
        })
    }
  }
}
</script>

<style lang="scss" scoped>
</style>
