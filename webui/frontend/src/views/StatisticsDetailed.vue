<template>
  <v-container fluid>
    <v-btn @click="returnView" class='mb-3' icon>
      <v-icon>mdi-arrow-left</v-icon>
    </v-btn>

    <v-card class='mb-3' dark>
      <v-card-text>

        <v-row justify='center' align='center'>
          <v-btn-toggle v-model="viewMode" dark>
            <v-btn text value='day'>
              Day
            </v-btn>
            <v-btn text value='hour'>
              Hour
            </v-btn>
          </v-btn-toggle>
        </v-row>

        <v-row justify='center' align='center'>
          <v-col cols='7' sm='4' md='2'>
            <v-menu
              v-model="menu1"
              :close-on-content-click="false"
              :nudge-right="40"
              transition="scale-transition"
              offset-y
              min-width="290px"
            >
              <template v-slot:activator="{ on }">
                <v-text-field
                  v-model.lazy="date"
                  class="mx-auto"
                  label="Day"
                  readonly
                  v-on="on"
                ></v-text-field>
              </template>
              <v-date-picker v-model="date" @input="menu1 = false"/>
            </v-menu>
          </v-col>

          <v-col v-if="viewMode === 'hour'" cols='7' sm='2' md='1'>
            <v-text-field
              v-model.lazy="hour"
              class="mx-auto"
              label="Hour"
              max="23"
              min="1"
              step="1"
              style="width: 125px"
              type="number"
            ></v-text-field>
          </v-col>
        </v-row>

      </v-card-text>
    </v-card>

    <v-card class="my-3">
      <v-card-title>
        Host
        <v-btn
          class="mx-3"
          @click='update'
          dark
          small
          icon
        >
          <v-icon>mdi-cached</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text>
        <v-row justify='center'>
          <v-col cols='5' lg='2'>
            <v-select
              :items="[
                { text: 'CPU Load', value: 'cpuLoad' },
                { text: 'RAM', value: 'ram' },
                { text: 'Storage', value: 'storage' },
                { text: 'Temperature', value: 'temperature' }
              ]"
              v-model="chart"
            />
          </v-col>
        </v-row>

        <app-line-chart
          :chart-data='rsChartData'
          :options='chartOptions'
        />
      </v-card-text>
    </v-card>
    <v-card class="my-3">
      <v-card-title>
        PiWorker
        <v-btn
          class="mx-3"
          @click='update'
          dark
          small
          icon
        >
          <v-icon>mdi-cached</v-icon>
        </v-btn>
      </v-card-title>
      <v-card-text>
        <app-line-chart
          :chart-data='tsChartData'
          :options='chartOptions'
        />
      </v-card-text>
    </v-card>
  </v-container>
</template>

<script>
import { mapGetters, mapActions } from 'vuex'
import LineChart from '../components/statistics-detailed/Chart.vue'

export default {
  data () {
    return {
      menu1: null,
      menu2: null,
      updating: false,

      chart: 'cpuLoad',

      chartOptions: {
        animation: {
          duration: 1
        },
        responsive: true,
        maintainAspectRatio: false
      }
    }
  },
  computed: {
    ...mapGetters('statistics', [
      'ts',
      'rs'
    ]),
    date: {
      get () {
        return this.$store.getters['statistics/date']
      },
      set (newValue) {
        this.$store.dispatch('statistics/setDate', newValue)
      }
    },
    hour: {
      get () {
        return this.$store.getters['statistics/hour']
      },
      set (newValue) {
        this.$store.dispatch('statistics/setHour', newValue)
      }
    },
    viewMode: {
      get () {
        return this.$store.getters['statistics/viewMode']
      },
      set (newValue) {
        this.$store.dispatch('statistics/setViewMode', newValue)
      }
    },
    rsChartData () {
      return {
        labels: this.rs.timestamps,
        datasets: this.rsDatasets
      }
    },
    tsChartData () {
      return {
        labels: this.ts.timestamps,
        datasets: [
          {
            label: 'Active Tasks',
            borderColor: 'rgb(29,160,22)',
            backgroundColor: 'rgba(29,160,22, 0.3)',
            data: this.ts.activeTasks
          },
          {
            label: 'Inactive Tasks',
            borderColor: 'rgb(160,151,22)',
            backgroundColor: 'rgba(160,151,22, 0.3)',
            data: this.ts.inactiveTasks
          },
          {
            label: 'On Execution Tasks',
            borderColor: 'rgb(30,67,122)',
            backgroundColor: 'rgba(30,67,122, 0.3)',
            data: this.ts.onExecutionTasks
          },
          {
            label: 'Failed Tasks',
            borderColor: 'rgb(119,47,35)',
            backgroundColor: 'rgba(119,47,35, 0.3)',
            data: this.ts.failedTasks
          }
        ]
      }
    },
    rsDatasets () {
      // According to user preferences return the appropriate datasets
      const datasets = []

      if (this.chart === 'cpuLoad') {
        datasets.push({
          label: 'CPU Load (%)',
          borderColor: 'rgb(52,152,221)',
          backgroundColor: 'rgba(52,152,221, 0.3)',
          data: this.rs.cpuLoad
        })
      }
      if (this.chart === 'ram') {
        datasets.push({
          label: 'RAM Available',
          borderColor: 'rgb(24,153,67)',
          backgroundColor: 'rgba(24,153,67, 0.3)',
          data: this.rs.rAvailable
        })
        datasets.push({
          label: 'RAM Used',
          borderColor: 'rgb(186,186,29)',
          backgroundColor: 'rgba(186,186,29, 0.3)',
          data: this.rs.rUsed
        })
      }
      if (this.chart === 'storage') {
        datasets.push({
          label: 'Storage free',
          borderColor: 'rgb(29,186,160)',
          backgroundColor: 'rgba(29,186,160, 0.3)',
          data: this.rs.sFree
        })
        datasets.push({
          label: 'Storage used',
          borderColor: 'rgb(115,29,186)',
          backgroundColor: 'rgba(115,29,186, 0.3)',
          data: this.rs.sUsed
        })
      }
      if (this.chart === 'temperature') {
        // TODO
      }

      return datasets
    }
  },
  watch: {
    date (newValue) {
      if (this.viewMode === 'hour' && this.hour == null) {
        return
      }

      const d = {
        date: newValue
      }

      if (this.viewMode === 'hour') {
        d.hour = this.hour
      }

      this.getStats(d)
    },
    hour (newValue) {
      if (this.date === '') {
        return
      }
      this.getStats({ date: this.date, hour: newValue })
    }
  },
  methods: {
    ...mapActions('statistics', [
      'getStats'
    ]),
    returnView () {
      this.$router.push({ name: 'statistics' })
    },
    update () {
      this.updating = true
      this.getStats({ from: this.from, to: this.to })
        .then(_ => {
          this.updating = false
        })
    }
  },
  components: {
    appLineChart: LineChart
  },
  beforeCreate () {
    if (this.$store.getters['statistics/date'] !== '') {
      return
    }

    let today = new Date()
    const dd = String(today.getDate()).padStart(2, '0')
    const mm = String(today.getMonth() + 1).padStart(2, '0')
    const yyyy = today.getFullYear()

    today = yyyy + '-' + mm + '-' + dd

    this.$store.dispatch('statistics/setDate', today)

    this.$store.dispatch('statistics/getStats', { date: today })
  }
}
</script>
