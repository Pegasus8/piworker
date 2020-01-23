<template>
<v-container class="my-4">
  <v-row justify='center'>
    <v-col sm="9" md="7" lg="5" xl="4">
      <v-card elevation='12' dark>
        <v-card-title>
          Sign in
        </v-card-title>
        <v-card-text>
          <v-form
            ref="form"
            v-model="valid"
          >
            <v-text-field
              v-model="form.username"
              :rules="usernameRules"
              label="Username"
              required
            />
            <v-text-field
              v-model="form.password"
              :rules="passwordRules"
              label="Password"
              type='password'
              required
            />
            <v-checkbox
              v-model="form.keepLogged"
              label="Keep me logged"
              color='primary'
            ></v-checkbox>

            <v-btn
              :disabled="!valid"
              class="primary darken-1 mt-2"
              @click="login"
              block
              :loading='waintingResponse'
            >
              Login
            </v-btn>
          </v-form>
        </v-card-text>
      </v-card>
    </v-col>
  </v-row>
  <v-row justify='center'>
    <v-fade-transition>
      <v-alert v-show="showAlert" type='error' class="mt-3">
        Error when trying to login. Please, check your connection with PiWorker.
        Error: <span class="font-weight-medium">{{ error }}</span>
      </v-alert>
    </v-fade-transition>
  </v-row>
</v-container>
</template>

<script>
export default {
  data () {
    return {
      form: {
        username: '',
        password: '',
        keepLogged: false
      },
      waintingResponse: false,
      usernameRules: [
        v => !!v || 'Username is required'
      ],
      passwordRules: [
        v => !!v || 'Username is required'
      ],
      valid: true,
      showAlert: false,
      error: ''
    }
  },
  methods: {
    login (event) {
      this.waintingResponse = true
      this.$store.dispatch('auth/login', {
        user: this.form.username,
        password: this.form.password
      })
        .then((response) => {
          this.waintingResponse = false
          if (!response.successful) {
            this.showAlert = true
            setTimeout(() => {
              this.showAlert = false
            }, 3000)
          }
        })
        .catch((err) => {
          this.waintingResponse = false
          this.showAlert = true
          this.error = err.message
          console.error('Error when trying to login:', err)
          setTimeout(() => {
            this.showAlert = false
            setTimeout(() => {
              // Prevents the disappearance of the error when the alert is beginning to fade
              this.error = ''
            }, 2000)
          }, 10000)
        })
    }
  }
}
</script>

<style lang="scss" scoped>
</style>
