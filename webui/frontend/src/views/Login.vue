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
                aria-describedby="usernamelHelp"
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
        <b-alert
          :show="showAlert"
          variant="warning"
          class="mt-3 text-left"
        >
          Wrong user/password
        </b-alert>
      </transition>
      <transition name="fade">
        <b-alert
          :show="errorOnLogin"
          variant="danger"
          class="mt-3 text-left"
        >
          <h4>Error when trying to login</h4>
          <p>Please, check your connection with PiWorker.</p>
          <hr>
          <p>Error: {{ error }}</p>
        </b-alert>
      </transition>
    </b-col>
  </b-row>
</b-container>
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